package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/techchapter/terraform-provider-wellbeing/internal/wellbeingclient"
)

// Story: Survey translation
//
// Input:  a wellbeing_survey configuration, plus the company's enabled languages.
// Process:
//   1. Resolve language codes to the numeric ids writes require.
//   2. Expand each localizable field from "scalar plus optional map" into the
//      id-keyed Texts map the API expects.
//   3. Synthesise the welcome and thankyou steps from first_page and last_page,
//      because the API demands them inside Questions and ignores the top-level
//      CoverPage and ThankYouPage objects.
//   4. Attach each question's server Key and each option's LabelKey, without
//      which an update appends duplicates instead of modifying.
// Output: a wellbeingclient.Survey ready to POST, or the reverse — a config
//         model rebuilt from a GET response.
//
// Dependencies: the enabled-languages list. No I/O of its own.
// Side effects: none.

// surveyResourceModel is the wellbeing_survey resource.
//
// The survey is immutable in its content: any change to name, the pages, or any
// question or answer replaces it. Only the scheduling attributes and state
// update in place. See classifySurveyChange.
type surveyResourceModel struct {
	ID              types.String `tfsdk:"id"`
	DefinitionID    types.Int64  `tfsdk:"definition_id"`
	Name            types.String `tfsdk:"name"`
	DefaultLanguage types.String `tfsdk:"default_language"`

	FirstPage      types.String `tfsdk:"first_page"`
	FirstPageTexts types.Map    `tfsdk:"first_page_texts"`
	LastPage       types.String `tfsdk:"last_page"`
	LastPageTexts  types.Map    `tfsdk:"last_page_texts"`

	Frequency types.String `tfsdk:"frequency"`
	Start     types.String `tfsdk:"start"`
	End       types.String `tfsdk:"end"`
	State     types.String `tfsdk:"state"`

	Question []surveyQuestionModel `tfsdk:"question"`
	Timeouts timeouts.Value        `tfsdk:"timeouts"`
}

// surveyQuestionModel is one `question` block.
//
// Key is the config-side identity used to match blocks across reorders.
// ServerKey is what Wellbeing assigned and what must go back on the wire.
type surveyQuestionModel struct {
	Key       types.String `tfsdk:"key"`
	ServerKey types.String `tfsdk:"server_key"`
	Type      types.String `tfsdk:"type"`
	Text      types.String `tfsdk:"text"`
	Texts     types.Map    `tfsdk:"texts"`

	Answer []surveyAnswerOptionModel `tfsdk:"answer"`
}

// surveyAnswerOptionModel is one `answer` block within an option question.
// Named to distinguish it from surveyAnswerModel, which is a recorded response
// in the answers data source.
type surveyAnswerOptionModel struct {
	Text           types.String `tfsdk:"text"`
	Texts          types.Map    `tfsdk:"texts"`
	ServerLabelKey types.String `tfsdk:"server_label_key"`
	Value          types.Int64  `tfsdk:"value"`
}

// languageResolver translates between the language codes used in HCL and the
// numeric ids the write side requires. Reads come back keyed by code, writes
// must be keyed by id, so both directions are needed.
type languageResolver struct {
	idByCode map[string]int64
	codeByID map[int64]string
}

func newLanguageResolver(languages []wellbeingclient.Language) languageResolver {
	resolver := languageResolver{
		idByCode: make(map[string]int64, len(languages)),
		codeByID: make(map[int64]string, len(languages)),
	}
	for _, language := range languages {
		resolver.idByCode[language.Code] = language.ID
		resolver.codeByID[language.ID] = language.Code
	}
	return resolver
}

func (r languageResolver) codes() []string { return sortedKeysOf(r.idByCode) }

func sortedKeysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// expandTexts turns the "scalar plus optional translations" pair into the
// id-keyed map the API wants.
//
// The framework has no union type, so a single attribute cannot accept both a
// string and a map. The scalar carries the default language and the map carries
// any others; the scalar always wins for the default language so the two cannot
// disagree.
func expandTexts(
	ctx context.Context,
	defaultLanguage string,
	scalar types.String,
	translations types.Map,
	resolver languageResolver,
	fieldPath string,
) (map[string]string, diag.Diagnostics) {
	var diags diag.Diagnostics

	byCode := map[string]string{}

	extra, mapDiags := mapToGo(ctx, translations)
	diags.Append(mapDiags...)
	if diags.HasError() {
		return nil, diags
	}
	for code, text := range extra {
		byCode[code] = text
	}
	if value := optionalString(scalar); value != nil {
		byCode[defaultLanguage] = *value
	}

	out := make(map[string]string, len(byCode))
	for _, code := range sortedKeysOf(byCode) {
		id, ok := resolver.idByCode[code]
		if !ok {
			diags.AddError(
				"Unknown language code",
				fmt.Sprintf("%s uses language %q, which is not enabled for this company. Enabled languages are: %s.\n\n"+
					"Enable it in the Wellbeing portal, or use one of the codes above.",
					fieldPath, code, strings.Join(resolver.codes(), ", ")),
			)
			continue
		}
		out[strconv.FormatInt(id, 10)] = byCode[code]
	}

	return out, diags
}

// collapseTexts is the inverse: it turns the API's per-language text back into
// the scalar plus translations pair, given the default language.
func collapseTexts(ctx context.Context, byCode map[string]string, defaultLanguage string) (types.String, types.Map, diag.Diagnostics) {
	var diags diag.Diagnostics

	scalar := types.StringNull()
	if text, ok := byCode[defaultLanguage]; ok {
		scalar = types.StringValue(text)
	}

	others := make(map[string]string, len(byCode))
	for code, text := range byCode {
		if code == defaultLanguage {
			continue
		}
		others[code] = text
	}
	if len(others) == 0 {
		return scalar, types.MapNull(types.StringType), diags
	}

	value, mapDiags := types.MapValueFrom(ctx, types.StringType, others)
	diags.Append(mapDiags...)
	return scalar, value, diags
}

// surveyTextsByCode reads every language's text for one step slot out of the
// API's Labels map.
func surveyTextsByCode(api wellbeingclient.Survey, questionKey, slot string) map[string]string {
	out := map[string]string{}
	for code := range api.Labels {
		if text := api.Text(code, questionKey, slot); text != "" {
			out[code] = text
		}
	}
	return out
}

// toAPISurvey converts a configuration into a payload the API accepts.
//
// The welcome and thankyou steps are synthesised here from first_page and
// last_page. The API requires both inside Questions and rejects a payload
// lacking either; the top-level CoverPage and ThankYouPage objects it also
// accepts are ignored, so they are deliberately not sent.
func toAPISurvey(
	ctx context.Context,
	plan surveyResourceModel,
	languages []wellbeingclient.Language,
) (wellbeingclient.Survey, diag.Diagnostics) {
	var diags diag.Diagnostics

	resolver := newLanguageResolver(languages)
	defaultLanguage := plan.DefaultLanguage.ValueString()

	survey := wellbeingclient.Survey{
		Name:         plan.Name.ValueString(),
		StartDate:    plan.Start.ValueString(),
		EndDate:      plan.End.ValueString(),
		Valid:        true,
		Filter:       map[string]any{},
		EnabledLangs: languages,
	}

	state, ok := surveyStateToAPI(plan.State.ValueString())
	if !ok {
		diags.AddError("Unknown survey state",
			fmt.Sprintf("state %q is not one of: %s.", plan.State.ValueString(), strings.Join(surveyStateValues(), ", ")))
		return survey, diags
	}
	survey.State = state

	frequency, ok := surveyFrequencyToAPI(plan.Frequency.ValueString())
	if !ok {
		diags.AddError("Unknown survey frequency",
			fmt.Sprintf("frequency %q is not one of: %s.\n\n"+
				"The Wellbeing API does not validate this field — it accepts any integer, including "+
				"meaningless ones — so the provider is the only thing standing between a typo and a "+
				"survey that never fires.",
				plan.Frequency.ValueString(), strings.Join(surveyFrequencyValues(), ", ")))
		return survey, diags
	}
	survey.Frequency = frequency

	if id := optionalString(plan.ID); id != nil {
		parsed, err := strconv.ParseInt(*id, 10, 64)
		if err == nil {
			survey.ID = parsed
		}
	}
	if !plan.DefinitionID.IsNull() && !plan.DefinitionID.IsUnknown() {
		survey.SurveyDefinitionID = plan.DefinitionID.ValueInt64()
	}

	// Selection rule mirrors the enabled languages. Audience targeting is out of
	// scope, so this is the equivalent of the legacy "everyone" rule.
	rule, ruleDiags := buildSelectionRule(languages)
	diags.Append(ruleDiags...)
	survey.SurveySelectionRule = rule

	questions := make([]wellbeingclient.SurveyQuestion, 0, len(plan.Question)+2)

	welcomeTexts, welcomeDiags := expandTexts(ctx, defaultLanguage, plan.FirstPage, plan.FirstPageTexts, resolver, "first_page")
	diags.Append(welcomeDiags...)
	questions = append(questions, wellbeingclient.SurveyQuestion{
		StepType:    wellbeingclient.StepTypeWelcome,
		Texts:       welcomeTexts,
		EditingLang: defaultLanguage,
		Valid:       true,
	})

	for index, question := range plan.Question {
		blockPath := fmt.Sprintf("question[%d]", index)

		stepType, ok := questionTypeToAPI(question.Type.ValueString())
		if !ok {
			diags.AddError("Unknown question type",
				fmt.Sprintf("%s has type %q, which is not one of: %s.",
					blockPath, question.Type.ValueString(), strings.Join(questionTypeValues(), ", ")))
			continue
		}

		texts, textDiags := expandTexts(ctx, defaultLanguage, question.Text, question.Texts, resolver, blockPath+".text")
		diags.Append(textDiags...)

		entry := wellbeingclient.SurveyQuestion{
			StepType:    stepType,
			Texts:       texts,
			EditingLang: defaultLanguage,
			Valid:       true,
			// Sending the server key is what makes an update modify the question
			// rather than append a copy of it.
			Key: question.ServerKey.ValueString(),
		}

		for answerIndex, answer := range question.Answer {
			answerPath := fmt.Sprintf("%s.answer[%d]", blockPath, answerIndex)
			answerTexts, answerDiags := expandTexts(ctx, defaultLanguage, answer.Text, answer.Texts, resolver, answerPath+".text")
			diags.Append(answerDiags...)

			option := wellbeingclient.SurveyAnswerOption{
				Texts:    answerTexts,
				Editing:  false,
				LabelKey: answer.ServerLabelKey.ValueString(),
			}
			if !answer.Value.IsNull() && !answer.Value.IsUnknown() {
				option.Value = int(answer.Value.ValueInt64())
			}
			entry.AnswerOptions = append(entry.AnswerOptions, option)
		}

		questions = append(questions, entry)
	}

	thankYouTexts, thankYouDiags := expandTexts(ctx, defaultLanguage, plan.LastPage, plan.LastPageTexts, resolver, "last_page")
	diags.Append(thankYouDiags...)
	questions = append(questions, wellbeingclient.SurveyQuestion{
		StepType:    wellbeingclient.StepTypeThankYou,
		Texts:       thankYouTexts,
		EditingLang: defaultLanguage,
		Valid:       true,
	})

	survey.Questions = questions
	return survey, diags
}

// buildSelectionRule renders the audience rule. Targeting is out of scope for
// this pass, so the rule carries only the enabled languages, matching what the
// legacy configuration sent.
func buildSelectionRule(languages []wellbeingclient.Language) (string, diag.Diagnostics) {
	var diags diag.Diagnostics

	encoded, err := jsonMarshal(languages)
	if err != nil {
		diags.AddError("Unable to build the survey selection rule", err.Error())
		return "", diags
	}
	rule, err := jsonMarshal(map[string]string{"EnabledLanguages": encoded})
	if err != nil {
		diags.AddError("Unable to build the survey selection rule", err.Error())
		return "", diags
	}
	return rule, diags
}

func jsonMarshal(v any) (string, error) {
	encoded, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// fromAPISurvey rebuilds the configuration model from a GET response.
//
// prior is the matching configured survey, or nil on import. When present, its
// question keys and default language are carried forward, because the API
// stores neither.
func fromAPISurvey(
	ctx context.Context,
	api wellbeingclient.Survey,
	prior *surveyResourceModel,
	languages []wellbeingclient.Language,
) (surveyResourceModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	defaultLanguage := ""
	if prior != nil {
		defaultLanguage = prior.DefaultLanguage.ValueString()
	}
	if defaultLanguage == "" && len(languages) > 0 {
		defaultLanguage = languages[0].Code
	}

	model := surveyResourceModel{
		ID:              types.StringValue(strconv.FormatInt(api.ID, 10)),
		DefinitionID:    types.Int64Value(api.SurveyDefinitionID),
		Name:            types.StringValue(api.Name),
		DefaultLanguage: types.StringValue(defaultLanguage),
		Start:           types.StringValue(api.StartDate),
		End:             types.StringValue(api.EndDate),
	}
	if prior != nil {
		model.Timeouts = prior.Timeouts
	}

	stateName, ok := surveyStateFromAPI(api.State)
	if !ok {
		diags.AddError("Unrecognised survey state from API",
			fmt.Sprintf("Survey %d has state %d, which the provider does not know how to represent. "+
				"This usually means the Wellbeing API added a value; please report it to the provider developers.",
				api.ID, api.State))
		return model, diags
	}
	model.State = types.StringValue(stateName)

	frequencyName, ok := surveyFrequencyFromAPI(api.Frequency)
	if !ok {
		diags.AddError("Unrecognised survey frequency from API",
			fmt.Sprintf("Survey %d has frequency %d, which the provider does not know how to represent. "+
				"The API does not validate this field, so the value may have been set outside Terraform. "+
				"Please report it to the provider developers.",
				api.ID, api.Frequency))
		return model, diags
	}
	model.Frequency = types.StringValue(frequencyName)

	// Match returned questions to prior blocks by server key so the configured
	// key and block order survive the round trip.
	priorByServerKey := map[string]surveyQuestionModel{}
	priorQuestions := []surveyQuestionModel(nil)
	if prior != nil {
		priorQuestions = prior.Question
		for _, question := range prior.Question {
			if key := question.ServerKey.ValueString(); key != "" {
				priorByServerKey[key] = question
			}
		}
	}

	// Immediately after a create the plan holds no server keys — the API has
	// only just assigned them — so a key-based match finds nothing and every
	// configured key would be replaced by a derived one, producing a diff on the
	// very next plan. The API returns questions in the order they were sent, so
	// falling back to position is correct here. Only do so when the counts agree;
	// if they do not, something changed server-side and deriving is the honest
	// answer.
	apiQuestionCount := 0
	for _, question := range api.Questions {
		if question.StepType != wellbeingclient.StepTypeWelcome && question.StepType != wellbeingclient.StepTypeThankYou {
			apiQuestionCount++
		}
	}
	positionalFallback := len(priorByServerKey) == 0 && len(priorQuestions) == apiQuestionCount

	ordered := slices.Clone(api.Questions)
	slices.SortStableFunc(ordered, func(a, b wellbeingclient.SurveyQuestion) int {
		return a.Order - b.Order
	})

	questions := make([]surveyQuestionModel, 0, len(ordered))

	for _, apiQuestion := range ordered {
		switch apiQuestion.StepType {
		case wellbeingclient.StepTypeWelcome:
			scalar, translations, pageDiags := collapseTexts(ctx,
				surveyTextsByCode(api, apiQuestion.Key, wellbeingclient.SurveySlotContent), defaultLanguage)
			diags.Append(pageDiags...)
			model.FirstPage, model.FirstPageTexts = scalar, translations
			continue

		case wellbeingclient.StepTypeThankYou:
			scalar, translations, pageDiags := collapseTexts(ctx,
				surveyTextsByCode(api, apiQuestion.Key, wellbeingclient.SurveySlotContent), defaultLanguage)
			diags.Append(pageDiags...)
			model.LastPage, model.LastPageTexts = scalar, translations
			continue
		}

		typeName, ok := questionTypeFromAPI(apiQuestion.StepType)
		if !ok {
			diags.AddError("Unrecognised question type from API",
				fmt.Sprintf("Survey %d question %q has step type %d, which the provider does not know how to represent. "+
					"Please report it to the provider developers.", api.ID, apiQuestion.Key, apiQuestion.StepType))
			return model, diags
		}

		scalar, translations, textDiags := collapseTexts(ctx,
			surveyTextsByCode(api, apiQuestion.Key, wellbeingclient.SurveySlotQuestion), defaultLanguage)
		diags.Append(textDiags...)

		question := surveyQuestionModel{
			ServerKey: types.StringValue(apiQuestion.Key),
			Type:      types.StringValue(typeName),
			Text:      scalar,
			Texts:     translations,
		}

		// Carry the configured key forward; the API does not store it.
		switch previous, ok := priorByServerKey[apiQuestion.Key]; {
		case ok:
			question.Key = previous.Key
		case positionalFallback && len(questions) < len(priorQuestions):
			question.Key = priorQuestions[len(questions)].Key
		default:
			question.Key = types.StringValue(deriveQuestionKey(scalar.ValueString()))
		}

		// The live option set comes from Configuration. Reading it from Labels
		// instead would resurrect options that were removed, because their label
		// entries are never cleaned up.
		config, err := apiQuestion.DecodeConfiguration()
		if err != nil {
			diags.AddError("Unable to read a question's answer options", err.Error())
			return model, diags
		}
		for _, option := range config.AnswerOptions {
			optionScalar, optionTranslations, optionDiags := collapseTexts(ctx,
				surveyTextsByCode(api, apiQuestion.Key, option.LabelKey), defaultLanguage)
			diags.Append(optionDiags...)

			question.Answer = append(question.Answer, surveyAnswerOptionModel{
				Text:           optionScalar,
				Texts:          optionTranslations,
				ServerLabelKey: types.StringValue(option.LabelKey),
				Value:          types.Int64Value(int64(option.Value)),
			})
		}

		questions = append(questions, question)
	}

	model.Question = questions
	return model, diags
}

// surveyChangeKind says how a configuration change must be applied.
type surveyChangeKind int

const (
	// surveyNoChange means state and config agree on everything that matters.
	surveyNoChange surveyChangeKind = iota
	// surveyUpdateInPlace means only scheduling or lifecycle attributes moved.
	surveyUpdateInPlace
	// surveyRequiresReplace means the survey's content changed, so a new survey
	// must be created and the old one deactivated.
	surveyRequiresReplace
)

func (k surveyChangeKind) String() string {
	switch k {
	case surveyUpdateInPlace:
		return "update-in-place"
	case surveyRequiresReplace:
		return "replace"
	default:
		return "no-change"
	}
}

// surveyContentFingerprint digests everything that, if changed, requires a new
// survey: the name, both pages, and every question and answer.
//
// Editing a live survey corrupts its own data — answers already recorded stay
// attached to a question that no longer asks what it asked when they were
// given. The provider cannot tell a typo fix from a change of meaning, so both
// are treated as replacement.
//
// Scheduling attributes are deliberately excluded: changing when a survey runs
// invalidates nothing that was already answered.
func surveyContentFingerprint(model surveyResourceModel) string {
	var b strings.Builder

	fmt.Fprintf(&b, "name=%s\n", model.Name.ValueString())
	fmt.Fprintf(&b, "lang=%s\n", model.DefaultLanguage.ValueString())
	fmt.Fprintf(&b, "first=%s|%s\n", model.FirstPage.ValueString(), mapFingerprint(model.FirstPageTexts))
	fmt.Fprintf(&b, "last=%s|%s\n", model.LastPage.ValueString(), mapFingerprint(model.LastPageTexts))

	for _, question := range model.Question {
		fmt.Fprintf(&b, "q:%s:%s:%s|%s\n",
			question.Key.ValueString(),
			question.Type.ValueString(),
			question.Text.ValueString(),
			mapFingerprint(question.Texts))
		for _, answer := range question.Answer {
			fmt.Fprintf(&b, "  a:%s|%s\n", answer.Text.ValueString(), mapFingerprint(answer.Texts))
		}
	}

	return b.String()
}

// mapFingerprint renders a translations map in a stable order. Go randomises map
// iteration, so an unsorted render would report spurious changes.
func mapFingerprint(value types.Map) string {
	if value.IsNull() || value.IsUnknown() {
		return ""
	}

	elements := value.Elements()
	keys := make([]string, 0, len(elements))
	for key := range elements {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	var b strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&b, "%s=%s;", key, elements[key].String())
	}
	return b.String()
}

// classifySurveyChange decides how to apply the difference between state and
// config.
//
// state is what Terraform last recorded; config is what the user now wants.
func classifySurveyChange(state, config surveyResourceModel) surveyChangeKind {
	if surveyContentFingerprint(state) != surveyContentFingerprint(config) {
		return surveyRequiresReplace
	}

	scheduling := []struct{ before, after types.String }{
		{state.Start, config.Start},
		{state.End, config.End},
		{state.Frequency, config.Frequency},
		{state.State, config.State},
	}
	for _, pair := range scheduling {
		if pair.before.ValueString() != pair.after.ValueString() {
			return surveyUpdateInPlace
		}
	}

	return surveyNoChange
}

// attachServerKeys copies the server-assigned identities from state onto a plan,
// matching by the configured key.
//
// This is what makes an update modify questions instead of duplicating them.
// A question whose key is absent from state keeps an empty server key, which
// tells the API to allocate a new one.
func attachServerKeys(plan *surveyResourceModel, state surveyResourceModel) {
	byKey := make(map[string]surveyQuestionModel, len(state.Question))
	for _, question := range state.Question {
		byKey[question.Key.ValueString()] = question
	}

	for i := range plan.Question {
		previous, ok := byKey[plan.Question[i].Key.ValueString()]
		if !ok {
			continue
		}
		plan.Question[i].ServerKey = previous.ServerKey

		optionsByText := make(map[string]surveyAnswerOptionModel, len(previous.Answer))
		for _, answer := range previous.Answer {
			optionsByText[answer.Text.ValueString()] = answer
		}
		for j := range plan.Question[i].Answer {
			prior, ok := optionsByText[plan.Question[i].Answer[j].Text.ValueString()]
			if !ok {
				continue
			}
			plan.Question[i].Answer[j].ServerLabelKey = prior.ServerLabelKey
			plan.Question[i].Answer[j].Value = prior.Value
		}
	}
}
