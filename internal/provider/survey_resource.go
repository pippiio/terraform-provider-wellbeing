package provider

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-timeouts/resource/timeouts"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/techchapter/terraform-provider-wellbeing/internal/wellbeingclient"
)

const defaultSurveyTimeout = 10 * time.Minute

var (
	_ resource.Resource                   = &surveyResource{}
	_ resource.ResourceWithConfigure      = &surveyResource{}
	_ resource.ResourceWithImportState    = &surveyResource{}
	_ resource.ResourceWithValidateConfig = &surveyResource{}
	_ resource.ResourceWithModifyPlan     = &surveyResource{}
)

// NewSurveyResource returns the survey resource.
func NewSurveyResource() resource.Resource {
	return &surveyResource{}
}

type surveyResource struct {
	client *wellbeingclient.Client
}

func (r *surveyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_survey"
}

func (r *surveyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a Wellbeing survey: its pages, questions and answer options.\n\n" +
			"~> **A survey is immutable in its content.** Changing the name, either page, or any " +
			"question or answer replaces the survey: a new one is created and the old one is " +
			"deactivated. This is deliberate. Editing a running survey would leave answers already " +
			"recorded attached to a question that no longer asks what it asked when they were given, " +
			"and the provider cannot tell a corrected typo from a changed meaning.\n\n" +
			"Only `start`, `end`, `frequency` and `state` update in place.\n\n" +
			"Because a replacement starts with no answer history, set " +
			"`lifecycle { create_before_destroy = true }` and expect `id` to change. Anything " +
			"referencing the old id — a `wellbeing_survey_answers` data source, for instance — must " +
			"be updated, or it silently reads the deactivated survey.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Survey ID assigned by Wellbeing. Changes whenever the survey is replaced.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"definition_id": schema.Int64Attribute{
				MarkdownDescription: "Survey definition ID. Needed to resolve question text, which the API " +
					"stores separately from the questions themselves.",
				Computed:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Survey name. Changing it replaces the survey — the API silently ignores " +
					"a name change on update.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"default_language": schema.StringAttribute{
				MarkdownDescription: "Language code that the plain `text`, `first_page` and `last_page` " +
					"attributes are written in, for example `da`. Must be enabled for the company; see the " +
					"`wellbeing_enabled_languages` data source.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"first_page": schema.StringAttribute{
				MarkdownDescription: "Cover page text, in `default_language`. Required: the API rejects a " +
					"survey without a cover page.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"first_page_texts": schema.MapAttribute{
				MarkdownDescription: "Cover page text in other languages, keyed by language code.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"last_page": schema.StringAttribute{
				MarkdownDescription: "Thank-you page text, in `default_language`. Required: the API rejects a " +
					"survey without a thank-you page.",
				Required:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"last_page_texts": schema.MapAttribute{
				MarkdownDescription: "Thank-you page text in other languages, keyed by language code.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"first_page_key": schema.StringAttribute{
				MarkdownDescription: "Identity Wellbeing assigned to the cover page. The API treats pages as " +
					"questions, so this is resent on every update; omitting it would append a second cover page.",
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"last_page_key": schema.StringAttribute{
				MarkdownDescription: "Identity Wellbeing assigned to the thank-you page.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"frequency": schema.StringAttribute{
				MarkdownDescription: "How often the survey runs. One of `hourly` or `quarterly`.\n\n" +
					"~> The API does not validate this field — it accepts any integer and stores it " +
					"verbatim — so the provider is the only thing preventing a value that quietly never fires.",
				Required:   true,
				Validators: []validator.String{stringvalidator.OneOf(surveyFrequencyValues()...)},
			},
			"start": schema.StringAttribute{
				MarkdownDescription: "Start of the survey window, as an ISO 8601 timestamp.",
				Required:            true,
			},
			"end": schema.StringAttribute{
				MarkdownDescription: "End of the survey window, as an ISO 8601 timestamp.",
				Required:            true,
			},
			"state": schema.StringAttribute{
				MarkdownDescription: "Lifecycle state: `draft`, `active` or `inactive`. Defaults to `draft` " +
					"so a survey does not reach employees until it is explicitly activated.\n\n" +
					"Destroying the resource moves the survey to `inactive` rather than deleting it; the " +
					"API has no delete, and answers recorded against an inactive survey are retained.",
				Optional:   true,
				Computed:   true,
				Default:    stringdefault.StaticString("draft"),
				Validators: []validator.String{stringvalidator.OneOf(surveyStateValues()...)},
			},
		},
		Blocks: map[string]schema.Block{
			"question": schema.ListNestedBlock{
				MarkdownDescription: "One block per question, in the order respondents see them. " +
					"The cover and thank-you pages are not questions — use `first_page` and `last_page`.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"key": schema.StringAttribute{
							MarkdownDescription: "Stable handle for this question, used to match it across " +
								"reordering. Derived from `text` when omitted.\n\n" +
								"Set it explicitly when two questions share the same wording — a derived key " +
								"would collide — or when you expect to reword the question later.",
							Optional:      true,
							Computed:      true,
							PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
						},
						"server_key": schema.StringAttribute{
							MarkdownDescription: "Identity Wellbeing assigned to this question. Sent back on " +
								"every update; omitting it would append a duplicate rather than modify.",
							Computed:      true,
							PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
						},
						"type": schema.StringAttribute{
							MarkdownDescription: "Question type: `option` for a fixed set of answers, `prompt` for free text.",
							Required:            true,
							Validators:          []validator.String{stringvalidator.OneOf(questionTypeValues()...)},
						},
						"text": schema.StringAttribute{
							MarkdownDescription: "Question text, in `default_language`.",
							Required:            true,
						},
						"texts": schema.MapAttribute{
							MarkdownDescription: "Question text in other languages, keyed by language code.",
							Optional:            true,
							ElementType:         types.StringType,
						},
					},
					Blocks: map[string]schema.Block{
						"answer": schema.ListNestedBlock{
							MarkdownDescription: "One block per selectable answer. Valid only when `type` is `option`.",
							NestedObject: schema.NestedBlockObject{
								Attributes: map[string]schema.Attribute{
									"text": schema.StringAttribute{
										MarkdownDescription: "Answer text, in `default_language`.",
										Required:            true,
									},
									"texts": schema.MapAttribute{
										MarkdownDescription: "Answer text in other languages, keyed by language code.",
										Optional:            true,
										ElementType:         types.StringType,
									},
									"server_label_key": schema.StringAttribute{
										MarkdownDescription: "Identity Wellbeing assigned to this option.",
										Computed:            true,
										PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
									},
									"value": schema.Int64Attribute{
										MarkdownDescription: "Numeric value Wellbeing records for this option.",
										Computed:            true,
										PlanModifiers:       []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
									},
								},
							},
						},
					},
				},
			},
			"timeouts": timeouts.Block(context.Background(), timeouts.Opts{Create: true, Update: true}),
		},
	}
}

func (r *surveyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*wellbeingclient.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected resource configure type",
			fmt.Sprintf("Expected *wellbeingclient.Client, got %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}
	r.client = client
}

// ValidateConfig catches at plan time what would otherwise be an opaque 500.
func (r *surveyResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config surveyResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	keys := make([]string, 0, len(config.Question))
	texts := make([]string, 0, len(config.Question))

	for index, question := range config.Question {
		blockPath := path.Root("question").AtListIndex(index)

		text := question.Text.ValueString()
		texts = append(texts, text)

		switch {
		case !question.Key.IsNull() && !question.Key.IsUnknown():
			keys = append(keys, question.Key.ValueString())
		case question.Text.IsUnknown():
			keys = append(keys, "")
		default:
			derived := deriveQuestionKey(text)
			keys = append(keys, derived)
			if derived == "" && text != "" {
				resp.Diagnostics.AddAttributeError(
					blockPath.AtName("key"),
					"Question key cannot be derived",
					fmt.Sprintf("question[%d] has text %q, which contains no characters usable in a key. Set key explicitly.", index, text),
				)
			}
		}

		// answer blocks only make sense on an option question.
		if question.Type.ValueString() == "prompt" && len(question.Answer) > 0 {
			resp.Diagnostics.AddAttributeError(
				blockPath.AtName("answer"),
				"Answer blocks on a free-text question",
				fmt.Sprintf("question[%d] has type \"prompt\" but declares %d answer block(s). "+
					"A prompt question collects free text and has no fixed answers. "+
					"Either remove the answer blocks or set type to \"option\".", index, len(question.Answer)),
			)
		}
		if question.Type.ValueString() == "option" && len(question.Answer) == 0 {
			resp.Diagnostics.AddAttributeError(
				blockPath.AtName("answer"),
				"Option question without answers",
				fmt.Sprintf("question[%d] has type \"option\" but declares no answer blocks. "+
					"Add at least one, or set type to \"prompt\" for free text.", index),
			)
		}
	}

	resp.Diagnostics.Append(questionKeyDiagnostics(keys, texts)...)
}

// ModifyPlan enforces the immutable-survey rule.
//
// A per-attribute RequiresReplace modifier covers the scalar fields, but a
// question or answer block appearing or disappearing is not an attribute change,
// so the nested blocks are compared here instead.
func (r *surveyResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		// Creating or destroying — nothing to compare.
		return
	}

	var state, plan surveyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if classifySurveyChange(state, plan) != surveyRequiresReplace {
		return
	}

	resp.RequiresReplace = append(resp.RequiresReplace, path.Root("question"))
	resp.Diagnostics.AddWarning(
		"Survey will be replaced, not edited",
		"A change to this survey's content means Terraform will create a new survey and deactivate "+
			"the current one.\n\n"+
			"The new survey starts with no answer history, and its id will differ. The old survey stays "+
			"readable and keeps its answers, but anything referencing the old id must be updated.\n\n"+
			"Set lifecycle { create_before_destroy = true } so the replacement exists before the current "+
			"survey is deactivated.",
	)
}

func (r *surveyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan surveyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	timeout, diags := plan.Timeouts.Create(ctx, defaultSurveyTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	fillDerivedKeys(&plan)

	languages, err := r.client.ListEnabledLanguages(ctx)
	if err != nil {
		addAPIErrorDiagnostic(&resp.Diagnostics, "Unable to read the company's enabled languages", err)
		return
	}

	payload, convertDiags := toAPISurvey(ctx, plan, languages)
	resp.Diagnostics.Append(convertDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := r.client.CreateSurvey(ctx, payload)
	if err != nil {
		addAPIErrorDiagnostic(&resp.Diagnostics, "Unable to create the Wellbeing survey", err)
		return
	}

	tflog.Info(ctx, "created wellbeing survey", map[string]any{"survey_id": id, "name": plan.Name.ValueString()})

	refreshed, readDiags := r.readInto(ctx, id, &plan, languages)
	resp.Diagnostics.Append(readDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &refreshed)...)
}

func (r *surveyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state surveyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := strconv.ParseInt(state.ID.ValueString(), 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid survey ID in state",
			fmt.Sprintf("State holds survey id %q, which is not a number.", state.ID.ValueString()))
		return
	}

	languages, err := r.client.ListEnabledLanguages(ctx)
	if err != nil {
		addAPIErrorDiagnostic(&resp.Diagnostics, "Unable to read the company's enabled languages", err)
		return
	}

	api, err := r.client.GetSurvey(ctx, id)
	if err != nil {
		var apiErr *wellbeingclient.APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == 404 {
			// Gone upstream: drop it from state so the next apply recreates it
			// rather than failing every plan.
			resp.State.RemoveResource(ctx)
			return
		}
		addAPIErrorDiagnostic(&resp.Diagnostics, "Unable to read the Wellbeing survey", err)
		return
	}

	refreshed, diags := fromAPISurvey(ctx, *api, &state, languages)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &refreshed)...)
}

// Update handles the in-place path only: scheduling and state.
//
// Content changes never reach here — ModifyPlan turns them into a replacement.
func (r *surveyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state surveyResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	timeout, diags := plan.Timeouts.Update(ctx, defaultSurveyTimeout)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	id, err := strconv.ParseInt(state.ID.ValueString(), 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid survey ID in state",
			fmt.Sprintf("State holds survey id %q, which is not a number.", state.ID.ValueString()))
		return
	}

	// The state change is a separate endpoint and is applied first, so that a
	// failure part-way leaves the survey in the state the user asked for.
	if plan.State.ValueString() != state.State.ValueString() {
		code, ok := surveyStateToAPI(plan.State.ValueString())
		if !ok {
			resp.Diagnostics.AddError("Unknown survey state",
				fmt.Sprintf("state %q is not one of: %s.", plan.State.ValueString(), strings.Join(surveyStateValues(), ", ")))
			return
		}
		if err := r.client.ChangeSurveyState(ctx, []int64{id}, code); err != nil {
			addAPIErrorDiagnostic(&resp.Diagnostics, "Unable to change the survey state", err)
			return
		}
	}

	languages, err := r.client.ListEnabledLanguages(ctx)
	if err != nil {
		addAPIErrorDiagnostic(&resp.Diagnostics, "Unable to read the company's enabled languages", err)
		return
	}

	// Carry the server-assigned identities over from state. Without them the
	// API appends a duplicate of every question instead of updating the survey,
	// and it demands the complete question set even for a date change.
	plan.ID, plan.DefinitionID = state.ID, state.DefinitionID
	attachServerKeys(&plan, state)

	payload, convertDiags := toAPISurvey(ctx, plan, languages)
	resp.Diagnostics.Append(convertDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.UpdateSurvey(ctx, payload); err != nil {
		addAPIErrorDiagnostic(&resp.Diagnostics, "Unable to update the Wellbeing survey", err)
		return
	}

	refreshed, readDiags := r.readInto(ctx, id, &plan, languages)
	resp.Diagnostics.Append(readDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &refreshed)...)
}

// Delete deactivates the survey. The API has no delete operation, and answers
// recorded against an inactive survey are retained.
func (r *surveyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state surveyResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := strconv.ParseInt(state.ID.ValueString(), 10, 64)
	if err != nil {
		resp.Diagnostics.AddError("Invalid survey ID in state",
			fmt.Sprintf("State holds survey id %q, which is not a number.", state.ID.ValueString()))
		return
	}

	if err := r.client.ChangeSurveyState(ctx, []int64{id}, wellbeingclient.StateInactive); err != nil {
		addAPIErrorDiagnostic(&resp.Diagnostics, "Unable to deactivate the Wellbeing survey", err)
		return
	}

	resp.Diagnostics.AddWarning(
		"Survey was deactivated, not deleted",
		fmt.Sprintf("Survey %q (id %s) has been moved to the inactive state and removed from Terraform "+
			"state. The Wellbeing API provides no delete operation, so the survey and every answer "+
			"recorded against it remain in Wellbeing and stay readable.\n\n"+
			"To manage it with Terraform again, run terraform import.",
			state.Name.ValueString(), state.ID.ValueString()),
	)
}

func (r *surveyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(strings.TrimSpace(req.ID), 10, 64)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			fmt.Sprintf("The import ID must be a numeric survey id, got %q.", req.ID),
		)
		return
	}

	// The detail response omits CompanyId, so ownership is checked against the
	// list endpoint, which is already scoped to the configured company.
	surveys, err := r.client.ListSurveys(ctx, 0)
	if err != nil {
		addAPIErrorDiagnostic(&resp.Diagnostics, "Unable to list the company's surveys", err)
		return
	}

	found := false
	for _, survey := range surveys {
		if survey.ID == id {
			found = true
			break
		}
	}
	if !found {
		resp.Diagnostics.AddError(
			"Survey does not belong to the configured company",
			fmt.Sprintf("Survey %d was not found among the surveys of company %s. "+
				"A survey can only be imported into the company the provider is configured for.",
				id, r.client.CompanyID()),
		)
		return
	}

	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// readInto refreshes a survey from the API, keeping prior as the source of
// configured spellings and keys.
func (r *surveyResource) readInto(
	ctx context.Context,
	id int64,
	prior *surveyResourceModel,
	languages []wellbeingclient.Language,
) (surveyResourceModel, diag.Diagnostics) {
	api, err := r.client.GetSurvey(ctx, id)
	if err != nil {
		var diags diag.Diagnostics
		addAPIErrorDiagnostic(&diags, "Unable to read back the Wellbeing survey", err)
		return surveyResourceModel{}, diags
	}
	return fromAPISurvey(ctx, *api, prior, languages)
}

// fillDerivedKeys assigns a key to every question that did not set one, so the
// value stored in state matches what the user would get from the derivation.
func fillDerivedKeys(plan *surveyResourceModel) {
	for i := range plan.Question {
		if plan.Question[i].Key.IsNull() || plan.Question[i].Key.IsUnknown() {
			plan.Question[i].Key = types.StringValue(deriveQuestionKey(plan.Question[i].Text.ValueString()))
		}
		if plan.Question[i].ServerKey.IsUnknown() {
			plan.Question[i].ServerKey = types.StringNull()
		}
		for j := range plan.Question[i].Answer {
			if plan.Question[i].Answer[j].ServerLabelKey.IsUnknown() {
				plan.Question[i].Answer[j].ServerLabelKey = types.StringNull()
			}
		}
	}
}
