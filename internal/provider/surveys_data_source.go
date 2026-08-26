package provider

import (
	"context"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/techchapter/terraform-provider-wellbeing/internal/wellbeingclient"
)

var (
	_ datasource.DataSource              = &surveysDataSource{}
	_ datasource.DataSourceWithConfigure = &surveysDataSource{}
)

// NewSurveysDataSource returns the surveys data source.
func NewSurveysDataSource() datasource.DataSource {
	return &surveysDataSource{}
}

type surveysDataSource struct {
	client *wellbeingclient.Client
}

type surveysDataSourceModel struct {
	ID      types.String         `tfsdk:"id"`
	State   types.String         `tfsdk:"state"`
	Surveys []surveySummaryModel `tfsdk:"surveys"`
}

type surveySummaryModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	State        types.String `tfsdk:"state"`
	Frequency    types.String `tfsdk:"frequency"`
	Start        types.String `tfsdk:"start"`
	End          types.String `tfsdk:"end"`
	DefinitionID types.Int64  `tfsdk:"definition_id"`
}

func (d *surveysDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_surveys"
}

func (d *surveysDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the company's surveys.\n\n" +
			"Useful for discovering the id of a survey created outside Terraform before importing it. " +
			"Questions are not included — the list endpoint omits them; use `terraform import` or read " +
			"the survey resource for the full definition.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The company ID the provider is configured with.",
				Computed:            true,
			},
			"state": schema.StringAttribute{
				MarkdownDescription: "Optional filter: only return surveys in this state (`draft`, `active` or `inactive`).",
				Optional:            true,
			},
			"surveys": schema.ListNestedAttribute{
				MarkdownDescription: "The surveys, in the order the API returns them.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":            schema.StringAttribute{Computed: true, MarkdownDescription: "Survey ID."},
						"name":          schema.StringAttribute{Computed: true, MarkdownDescription: "Survey name."},
						"state":         schema.StringAttribute{Computed: true, MarkdownDescription: "Lifecycle state."},
						"frequency":     schema.StringAttribute{Computed: true, MarkdownDescription: "How often the survey runs."},
						"start":         schema.StringAttribute{Computed: true, MarkdownDescription: "Start of the survey window."},
						"end":           schema.StringAttribute{Computed: true, MarkdownDescription: "End of the survey window."},
						"definition_id": schema.Int64Attribute{Computed: true, MarkdownDescription: "Survey definition ID."},
					},
				},
			},
		},
	}
}

func (d *surveysDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureDataSourceClient(req, resp)
}

func (d *surveysDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config surveysDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	stateFilter := 0
	if name := optionalString(config.State); name != nil {
		code, ok := surveyStateToAPI(*name)
		if !ok {
			resp.Diagnostics.AddError("Unknown survey state", "state must be draft, active or inactive.")
			return
		}
		stateFilter = code
	}

	surveys, err := d.client.ListSurveys(ctx, stateFilter)
	if err != nil {
		addAPIErrorDiagnostic(&resp.Diagnostics, "Unable to list Wellbeing surveys", err)
		return
	}

	config.ID = types.StringValue(d.client.CompanyID())
	config.Surveys = make([]surveySummaryModel, 0, len(surveys))

	for _, survey := range surveys {
		summary := surveySummaryModel{
			ID:           types.StringValue(strconv.FormatInt(survey.ID, 10)),
			Name:         types.StringValue(survey.Name),
			Start:        types.StringValue(survey.StartDate),
			End:          types.StringValue(survey.EndDate),
			DefinitionID: types.Int64Value(survey.SurveyDefinitionID),
			State:        types.StringNull(),
			Frequency:    types.StringNull(),
		}
		// Surveys created outside Terraform may carry values the provider does
		// not have a name for. Reporting them as null beats failing the read.
		if name, ok := surveyStateFromAPI(survey.State); ok {
			summary.State = types.StringValue(name)
		}
		if name, ok := surveyFrequencyFromAPI(survey.Frequency); ok {
			summary.Frequency = types.StringValue(name)
		}
		config.Surveys = append(config.Surveys, summary)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
