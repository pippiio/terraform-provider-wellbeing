package provider

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/techchapter/terraform-provider-wellbeing/internal/wellbeingclient"
)

func testLanguages() []wellbeingclient.Language {
	return []wellbeingclient.Language{
		{ID: 1041, Label: "Danish", Code: "da"},
		{ID: 1045, Label: "English", Code: "en"},
	}
}

func stringMapValue(t *testing.T, in map[string]string) types.Map {
	t.Helper()

	if in == nil {
		return types.MapNull(types.StringType)
	}
	value, diags := types.MapValueFrom(context.Background(), types.StringType, in)
	if diags.HasError() {
		t.Fatalf("building map: %v", diags)
	}
	return value
}

// minimalSurvey is a two-question survey used as the baseline across these
// tests. Content is fictional.
func minimalSurvey(t *testing.T) surveyResourceModel {
	t.Helper()

	return surveyResourceModel{
		Name:            types.StringValue("Trivsel"),
		DefaultLanguage: types.StringValue("da"),
		FirstPage:       types.StringValue("Velkommen"),
		FirstPageTexts:  types.MapNull(types.StringType),
		LastPage:        types.StringValue("Tak for svarene"),
		LastPageTexts:   types.MapNull(types.StringType),
		Frequency:       types.StringValue("quarterly"),
		Start:           types.StringValue("2030-01-01T00:00:00Z"),
		End:             types.StringValue("2030-12-31T00:00:00Z"),
		State:           types.StringValue("draft"),
		Question: []surveyQuestionModel{
			{
				Key:       types.StringValue("tilfreds"),
				ServerKey: types.StringNull(),
				Type:      types.StringValue("option"),
				Text:      types.StringValue("Er du tilfreds?"),
				Texts:     types.MapNull(types.StringType),
				Answer: []surveyAnswerOptionModel{
					{Text: types.StringValue("Ja"), Texts: types.MapNull(types.StringType), ServerLabelKey: types.StringNull(), Value: types.Int64Null()},
					{Text: types.StringValue("Nej"), Texts: types.MapNull(types.StringType), ServerLabelKey: types.StringNull(), Value: types.Int64Null()},
				},
			},
			{
				Key:       types.StringValue("uddyb"),
				ServerKey: types.StringNull(),
				Type:      types.StringValue("prompt"),
				Text:      types.StringValue("Uddyb gerne"),
				Texts:     types.MapNull(types.StringType),
			},
		},
	}
}

func TestToAPISurveySynthesisesWelcomeAndThankYouSteps(t *testing.T) {
	// The API requires a welcome and a thankyou step inside Questions and
	// rejects a payload without them.
	api, diags := toAPISurvey(context.Background(), minimalSurvey(t), testLanguages())

	if diags.HasError() {
		t.Fatalf("toAPISurvey: %v", diags)
	}
	if len(api.Questions) != 4 {
		t.Fatalf("Questions = %d, want 4 (welcome + 2 + thankyou)", len(api.Questions))
	}
	if api.Questions[0].StepType != wellbeingclient.StepTypeWelcome {
		t.Errorf("first step = %d, want welcome", api.Questions[0].StepType)
	}
	if api.Questions[3].StepType != wellbeingclient.StepTypeThankYou {
		t.Errorf("last step = %d, want thankyou", api.Questions[3].StepType)
	}
}

func TestToAPISurveyDoesNotSendCoverPageObjects(t *testing.T) {
	// The API ignores CoverPage/ThankYouPage and rejects a payload that carries
	// only them, so the provider never emits them. Asserted on the marshalled
	// form because the fields do not exist on the struct at all.
	api, _ := toAPISurvey(context.Background(), minimalSurvey(t), testLanguages())

	encoded, err := json.Marshal(api)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	for _, forbidden := range []string{"CoverPage", "ThankYouPage"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Errorf("payload contains %q, which the API ignores", forbidden)
		}
	}
}

func TestToAPISurveyKeysTextsByNumericLanguageID(t *testing.T) {
	// Writes address languages by id even though reads return codes.
	api, diags := toAPISurvey(context.Background(), minimalSurvey(t), testLanguages())
	if diags.HasError() {
		t.Fatalf("toAPISurvey: %v", diags)
	}

	texts := api.Questions[1].Texts
	if _, ok := texts["1041"]; !ok {
		t.Errorf("Texts keys = %v, want the numeric id 1041", keysOfStringMap(texts))
	}
	if _, ok := texts["da"]; ok {
		t.Error("Texts is keyed by language code, but writes require numeric ids")
	}
}

func TestToAPISurveyExpandsTranslations(t *testing.T) {
	model := minimalSurvey(t)
	model.Question[0].Texts = stringMapValue(t, map[string]string{"en": "Are you satisfied?"})

	api, diags := toAPISurvey(context.Background(), model, testLanguages())
	if diags.HasError() {
		t.Fatalf("toAPISurvey: %v", diags)
	}

	texts := api.Questions[1].Texts
	if texts["1041"] != "Er du tilfreds?" {
		t.Errorf("Danish text = %q", texts["1041"])
	}
	if texts["1045"] != "Are you satisfied?" {
		t.Errorf("English text = %q", texts["1045"])
	}
}

func TestToAPISurveyRejectsAnUnenabledLanguage(t *testing.T) {
	model := minimalSurvey(t)
	model.Question[0].Texts = stringMapValue(t, map[string]string{"fr": "Bonjour"})

	_, diags := toAPISurvey(context.Background(), model, testLanguages())

	if !diags.HasError() {
		t.Fatal("expected an error for a language the company has not enabled")
	}
	detail := diags.Errors()[0].Detail()
	for _, want := range []string{"fr", "da", "en"} {
		if !strings.Contains(detail, want) {
			t.Errorf("detail %q should mention %q", detail, want)
		}
	}
}

func TestToAPISurveyRejectsAnUnknownFrequency(t *testing.T) {
	// The API accepts any integer here, so this guard exists only in the provider.
	model := minimalSurvey(t)
	model.Frequency = types.StringValue("fortnightly")

	_, diags := toAPISurvey(context.Background(), model, testLanguages())

	if !diags.HasError() {
		t.Fatal("expected an error for an unknown frequency")
	}
	if !strings.Contains(diags.Errors()[0].Detail(), "quarterly") {
		t.Errorf("detail should list the accepted values, got %q", diags.Errors()[0].Detail())
	}
}

func TestToAPISurveySendsServerKeysWhenPresent(t *testing.T) {
	// This is the guard against the duplicate-append failure: an update that
	// omits Key does not fail, it silently appends a copy of every question.
	model := minimalSurvey(t)
	model.Question[0].ServerKey = types.StringValue("Q2")
	model.Question[1].ServerKey = types.StringValue("Q3")

	api, diags := toAPISurvey(context.Background(), model, testLanguages())
	if diags.HasError() {
		t.Fatalf("toAPISurvey: %v", diags)
	}

	if api.Questions[1].Key != "Q2" {
		t.Errorf("first question Key = %q, want Q2", api.Questions[1].Key)
	}
	if api.Questions[2].Key != "Q3" {
		t.Errorf("second question Key = %q, want Q3", api.Questions[2].Key)
	}
}

func TestFromAPISurveyStripsPagesOutOfQuestions(t *testing.T) {
	api := loadSurveyFixture(t)

	model, diags := fromAPISurvey(context.Background(), api, nil, testLanguages())
	if diags.HasError() {
		t.Fatalf("fromAPISurvey: %v", diags)
	}

	// The fixture holds welcome + option + prompt + thankyou.
	if len(model.Question) != 2 {
		t.Fatalf("Question blocks = %d, want 2 — the pages must not appear as questions", len(model.Question))
	}
	if model.FirstPage.ValueString() != "Velkommen probe" {
		t.Errorf("FirstPage = %q", model.FirstPage.ValueString())
	}
	if model.LastPage.ValueString() != "Tak probe" {
		t.Errorf("LastPage = %q", model.LastPage.ValueString())
	}
}

func TestFromAPISurveyReadsOptionsFromConfigurationNotLabels(t *testing.T) {
	// Removing an option drops it from Configuration but leaves its label
	// behind. Reading options from Labels would resurrect deleted options.
	api := loadSurveyFixture(t)

	// Simulate an orphaned label from an option that was removed.
	api.Labels["da"][wellbeingclient.SurveyLabelPath(api.SurveyDefinitionID, "Q2", "Option9")] = "SLETTET"

	model, diags := fromAPISurvey(context.Background(), api, nil, testLanguages())
	if diags.HasError() {
		t.Fatalf("fromAPISurvey: %v", diags)
	}

	for _, question := range model.Question {
		for _, answer := range question.Answer {
			if answer.Text.ValueString() == "SLETTET" {
				t.Fatal("a removed option was resurrected from an orphaned label")
			}
		}
	}
}

func TestFromAPISurveyCarriesConfiguredKeysForward(t *testing.T) {
	// The API does not store the config-side key, so it must survive via state.
	api := loadSurveyFixture(t)
	prior := surveyResourceModel{
		DefaultLanguage: types.StringValue("da"),
		Question: []surveyQuestionModel{
			{Key: types.StringValue("min-egen-noegle"), ServerKey: types.StringValue("Q2")},
		},
	}

	model, diags := fromAPISurvey(context.Background(), api, &prior, testLanguages())
	if diags.HasError() {
		t.Fatalf("fromAPISurvey: %v", diags)
	}

	if got := model.Question[0].Key.ValueString(); got != "min-egen-noegle" {
		t.Errorf("Key = %q, want the configured key carried forward", got)
	}
}

func TestFromAPISurveyDerivesKeyOnImport(t *testing.T) {
	// With no prior state there is nothing to carry forward, so the key comes
	// from the question text.
	api := loadSurveyFixture(t)

	model, diags := fromAPISurvey(context.Background(), api, nil, testLanguages())
	if diags.HasError() {
		t.Fatalf("fromAPISurvey: %v", diags)
	}

	if got := model.Question[0].Key.ValueString(); got != "foerste-spoergsmaal" {
		t.Errorf("derived key = %q, want foerste-spoergsmaal", got)
	}
}

func TestFromAPISurveyPreservesServerKeys(t *testing.T) {
	api := loadSurveyFixture(t)

	model, _ := fromAPISurvey(context.Background(), api, nil, testLanguages())

	if got := model.Question[0].ServerKey.ValueString(); got != "Q2" {
		t.Errorf("ServerKey = %q, want Q2", got)
	}
	if len(model.Question[0].Answer) == 0 {
		t.Fatal("option question has no answers")
	}
	if got := model.Question[0].Answer[0].ServerLabelKey.ValueString(); got != "Option1" {
		t.Errorf("ServerLabelKey = %q, want Option1", got)
	}
}

func TestFromAPISurveyRejectsAnUnknownStepType(t *testing.T) {
	api := loadSurveyFixture(t)
	api.Questions[1].StepType = 42

	_, diags := fromAPISurvey(context.Background(), api, nil, testLanguages())

	if !diags.HasError() {
		t.Fatal("expected an error for an unrecognised step type")
	}
	if !strings.Contains(diags.Errors()[0].Detail(), "report") {
		t.Error("an unknown API value should ask the user to report it, not default silently")
	}
}

func TestSurveyRoundTripPreservesContent(t *testing.T) {
	// The property that protects an empty plan on an unchanged survey.
	original := minimalSurvey(t)
	original.Question[0].Texts = stringMapValue(t, map[string]string{"en": "Are you satisfied?"})

	api, diags := toAPISurvey(context.Background(), original, testLanguages())
	if diags.HasError() {
		t.Fatalf("toAPISurvey: %v", diags)
	}

	// Reflect the payload back the way the API would return it.
	returned := simulateAPIResponse(t, api)

	rebuilt, diags := fromAPISurvey(context.Background(), returned, &original, testLanguages())
	if diags.HasError() {
		t.Fatalf("fromAPISurvey: %v", diags)
	}

	if got, want := surveyContentFingerprint(rebuilt), surveyContentFingerprint(original); got != want {
		t.Errorf("round trip changed the content\n got: %s\nwant: %s", got, want)
	}
}

func TestClassifySurveyChangeDetectsNoChange(t *testing.T) {
	model := minimalSurvey(t)

	if got := classifySurveyChange(model, model); got != surveyNoChange {
		t.Errorf("classify = %s, want no-change", got)
	}
}

func TestClassifySurveyChangeTreatsSchedulingAsInPlace(t *testing.T) {
	state := minimalSurvey(t)

	tests := []struct {
		name  string
		apply func(*surveyResourceModel)
	}{
		{name: "end date", apply: func(m *surveyResourceModel) { m.End = types.StringValue("2031-01-01T00:00:00Z") }},
		{name: "start date", apply: func(m *surveyResourceModel) { m.Start = types.StringValue("2030-06-01T00:00:00Z") }},
		{name: "frequency", apply: func(m *surveyResourceModel) { m.Frequency = types.StringValue("hourly") }},
		{name: "state", apply: func(m *surveyResourceModel) { m.State = types.StringValue("active") }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := minimalSurvey(t)
			test.apply(&config)

			if got := classifySurveyChange(state, config); got != surveyUpdateInPlace {
				t.Errorf("classify = %s, want update-in-place", got)
			}
		})
	}
}

func TestClassifySurveyChangeTreatsContentAsReplace(t *testing.T) {
	// Any change to what the survey asks replaces it, because edits corrupt the
	// answers already recorded against the old wording.
	state := minimalSurvey(t)

	tests := []struct {
		name  string
		apply func(*surveyResourceModel)
	}{
		{name: "question text", apply: func(m *surveyResourceModel) { m.Question[0].Text = types.StringValue("Er du glad?") }},
		{name: "answer text", apply: func(m *surveyResourceModel) { m.Question[0].Answer[0].Text = types.StringValue("Måske") }},
		{name: "question added", apply: func(m *surveyResourceModel) {
			m.Question = append(m.Question, surveyQuestionModel{
				Key: types.StringValue("ny"), Type: types.StringValue("prompt"),
				Text: types.StringValue("Ny"), Texts: types.MapNull(types.StringType),
			})
		}},
		{name: "question removed", apply: func(m *surveyResourceModel) { m.Question = m.Question[:1] }},
		{name: "question reordered", apply: func(m *surveyResourceModel) {
			m.Question[0], m.Question[1] = m.Question[1], m.Question[0]
		}},
		{name: "answer removed", apply: func(m *surveyResourceModel) { m.Question[0].Answer = m.Question[0].Answer[:1] }},
		{name: "name", apply: func(m *surveyResourceModel) { m.Name = types.StringValue("Andet navn") }},
		{name: "first page", apply: func(m *surveyResourceModel) { m.FirstPage = types.StringValue("Hej") }},
		{name: "last page", apply: func(m *surveyResourceModel) { m.LastPage = types.StringValue("Farvel") }},
		{name: "question type", apply: func(m *surveyResourceModel) { m.Question[1].Type = types.StringValue("option") }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := minimalSurvey(t)
			test.apply(&config)

			if got := classifySurveyChange(state, config); got != surveyRequiresReplace {
				t.Errorf("classify = %s, want replace", got)
			}
		})
	}
}

func TestSurveyContentFingerprintIgnoresMapOrdering(t *testing.T) {
	// Go randomises map iteration; an unsorted digest would report phantom drift.
	a := minimalSurvey(t)
	a.Question[0].Texts = stringMapValue(t, map[string]string{"en": "One", "da": "En"})
	b := minimalSurvey(t)
	b.Question[0].Texts = stringMapValue(t, map[string]string{"da": "En", "en": "One"})

	if surveyContentFingerprint(a) != surveyContentFingerprint(b) {
		t.Error("fingerprint depends on map ordering")
	}
}

func TestAttachServerKeysCopiesIdentityFromState(t *testing.T) {
	state := minimalSurvey(t)
	state.Question[0].ServerKey = types.StringValue("Q2")
	state.Question[0].Answer[0].ServerLabelKey = types.StringValue("Option1")
	state.Question[1].ServerKey = types.StringValue("Q3")

	plan := minimalSurvey(t)
	attachServerKeys(&plan, state)

	if got := plan.Question[0].ServerKey.ValueString(); got != "Q2" {
		t.Errorf("ServerKey = %q, want Q2", got)
	}
	if got := plan.Question[0].Answer[0].ServerLabelKey.ValueString(); got != "Option1" {
		t.Errorf("ServerLabelKey = %q, want Option1", got)
	}
}

func TestAttachServerKeysLeavesNewQuestionsUnkeyed(t *testing.T) {
	// A question absent from state must stay unkeyed so the server allocates one.
	state := minimalSurvey(t)
	state.Question[0].ServerKey = types.StringValue("Q2")
	state.Question = state.Question[:1]

	plan := minimalSurvey(t)
	attachServerKeys(&plan, state)

	if plan.Question[1].ServerKey.ValueString() != "" {
		t.Errorf("a question new to this apply must carry no server key, got %q",
			plan.Question[1].ServerKey.ValueString())
	}
}

func TestAttachServerKeysSurvivesReordering(t *testing.T) {
	// Matching is by configured key, so moving a block must not move its identity.
	state := minimalSurvey(t)
	state.Question[0].ServerKey = types.StringValue("Q2")
	state.Question[1].ServerKey = types.StringValue("Q3")

	plan := minimalSurvey(t)
	plan.Question[0], plan.Question[1] = plan.Question[1], plan.Question[0]
	attachServerKeys(&plan, state)

	if got := plan.Question[0].Key.ValueString(); got != "uddyb" {
		t.Fatalf("test setup wrong: first block key = %q", got)
	}
	if got := plan.Question[0].ServerKey.ValueString(); got != "Q3" {
		t.Errorf("reordered block took the wrong server key: %q, want Q3", got)
	}
}

// --- helpers ---

func loadSurveyFixture(t *testing.T) wellbeingclient.Survey {
	t.Helper()

	raw, err := os.ReadFile("../wellbeingclient/testdata/survey-detail.json")
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	var survey wellbeingclient.Survey
	if err := json.Unmarshal(raw, &survey); err != nil {
		t.Fatalf("decoding fixture: %v", err)
	}
	return survey
}

// simulateAPIResponse reflects a write payload back in the shape a read returns:
// text moved into Labels keyed by language code, options moved into a
// JSON-encoded Configuration string, and server keys assigned positionally.
func simulateAPIResponse(t *testing.T, sent wellbeingclient.Survey) wellbeingclient.Survey {
	t.Helper()

	codeByID := map[string]string{"1041": "da", "1045": "en"}

	out := wellbeingclient.Survey{
		ID: 1, SurveyDefinitionID: 2,
		Name: sent.Name, StartDate: sent.StartDate, EndDate: sent.EndDate,
		State: sent.State, Frequency: sent.Frequency,
		Labels: map[string]map[string]string{},
	}

	for index, question := range sent.Questions {
		key := "Q" + itoa(index+1)
		entry := wellbeingclient.SurveyQuestion{Key: key, Order: index + 1, StepType: question.StepType}

		slot := wellbeingclient.SurveySlotQuestion
		if question.StepType == wellbeingclient.StepTypeWelcome || question.StepType == wellbeingclient.StepTypeThankYou {
			slot = wellbeingclient.SurveySlotContent
		}
		for id, text := range question.Texts {
			code := codeByID[id]
			if out.Labels[code] == nil {
				out.Labels[code] = map[string]string{}
			}
			out.Labels[code][wellbeingclient.SurveyLabelPath(out.SurveyDefinitionID, key, slot)] = text
		}

		if len(question.AnswerOptions) > 0 {
			config := wellbeingclient.SurveyQuestionConfiguration{}
			for optionIndex, option := range question.AnswerOptions {
				labelKey := wellbeingclient.SurveySlotOption(optionIndex + 1)
				config.AnswerOptions = append(config.AnswerOptions, wellbeingclient.SurveyAnswerOption{
					LabelKey: labelKey, Value: optionIndex + 1,
				})
				for id, text := range option.Texts {
					code := codeByID[id]
					if out.Labels[code] == nil {
						out.Labels[code] = map[string]string{}
					}
					out.Labels[code][wellbeingclient.SurveyLabelPath(out.SurveyDefinitionID, key, labelKey)] = text
				}
			}
			encoded, err := json.Marshal(config)
			if err != nil {
				t.Fatalf("encoding configuration: %v", err)
			}
			raw := string(encoded)
			entry.Configuration = &raw
		}

		out.Questions = append(out.Questions, entry)
	}

	return out
}

func itoa(n int) string {
	return strings.TrimSpace(strings.Join(strings.Fields(jsonNumber(n)), ""))
}

func jsonNumber(n int) string {
	encoded, _ := json.Marshal(n)
	return string(encoded)
}

func keysOfStringMap(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
