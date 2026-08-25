package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/techchapter/terraform-provider-wellbeing/internal/wellbeingclient"
)

var (
	_ datasource.DataSource              = &surveyAnswersDataSource{}
	_ datasource.DataSourceWithConfigure = &surveyAnswersDataSource{}
)

// NewSurveyAnswersDataSource returns the survey answers data source.
func NewSurveyAnswersDataSource() datasource.DataSource {
	return &surveyAnswersDataSource{}
}

type surveyAnswersDataSource struct {
	client *wellbeingclient.Client
}

type surveyAnswersModel struct {
	SurveyID     types.String        `tfsdk:"survey_id"`
	DepartmentID types.String        `tfsdk:"department_id"`
	Period       types.String        `tfsdk:"period"`
	Answers      []surveyAnswerModel `tfsdk:"answers"`
}

type surveyAnswerModel struct {
	SurveyID    types.Int64  `tfsdk:"survey_id"`
	QuestionKey types.String `tfsdk:"question_key"`
	Answer      types.String `tfsdk:"answer"`
	Time        types.String `tfsdk:"time"`
	Period      types.String `tfsdk:"period"`
	Count       types.Int64  `tfsdk:"count"`
}

func (d *surveyAnswersDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_survey_answers"
}

func (d *surveyAnswersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Aggregated survey answers for the company.\n\n" +
			"Selected-option answers are grouped: one entry per option, carrying a `count` and a null `time`. " +
			"Free-text answers always have `count` 1 and carry the answer `time`.",
		Attributes: map[string]schema.Attribute{
			"survey_id": schema.StringAttribute{
				MarkdownDescription: "Restrict results to one survey.",
				Optional:            true,
			},
			"department_id": schema.StringAttribute{
				MarkdownDescription: "Restrict results to one department.",
				Optional:            true,
			},
			"period": schema.StringAttribute{
				MarkdownDescription: "Reporting period. `YYYY-Q1` through `YYYY-Q4` for quarterly surveys, otherwise `YYYY-M`.",
				Optional:            true,
			},
			"answers": schema.ListNestedAttribute{
				MarkdownDescription: "Matching answers.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"survey_id":    schema.Int64Attribute{Computed: true},
						"question_key": schema.StringAttribute{Computed: true},
						"answer":       schema.StringAttribute{Computed: true},
						"time":         schema.StringAttribute{Computed: true},
						"period":       schema.StringAttribute{Computed: true},
						"count":        schema.Int64Attribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *surveyAnswersDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureDataSourceClient(req, resp)
}

func (d *surveyAnswersDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config surveyAnswersModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	answers, err := d.client.ListSurveyAnswers(ctx, wellbeingclient.SurveyAnswerFilter{
		SurveyID:     config.SurveyID.ValueString(),
		DepartmentID: config.DepartmentID.ValueString(),
		Period:       config.Period.ValueString(),
	})
	if err != nil {
		addAPIErrorDiagnostic(&resp.Diagnostics, "Unable to read Wellbeing survey answers", err)
		return
	}

	config.Answers = make([]surveyAnswerModel, 0, len(answers))
	for _, answer := range answers {
		config.Answers = append(config.Answers, surveyAnswerModel{
			SurveyID:    types.Int64Value(answer.SurveyID),
			QuestionKey: types.StringValue(answer.QuestionKey),
			Answer:      types.StringValue(answer.Answer),
			Time:        stringOrNull(answer.Time),
			Period:      types.StringValue(answer.Period),
			Count:       types.Int64Value(answer.Count),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
