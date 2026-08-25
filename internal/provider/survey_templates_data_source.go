package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/techchapter/terraform-provider-wellbeing/internal/wellbeingclient"
)

var (
	_ datasource.DataSource              = &surveyTemplatesDataSource{}
	_ datasource.DataSourceWithConfigure = &surveyTemplatesDataSource{}
)

// NewSurveyTemplatesDataSource returns the survey templates data source.
func NewSurveyTemplatesDataSource() datasource.DataSource {
	return &surveyTemplatesDataSource{}
}

type surveyTemplatesDataSource struct {
	client *wellbeingclient.Client
}

type surveyTemplatesModel struct {
	Templates []surveyTemplateModel `tfsdk:"templates"`
}

type surveyTemplateModel struct {
	ID   types.Int64  `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

func (d *surveyTemplatesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_survey_templates"
}

func (d *surveyTemplatesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Survey templates available to the company.",
		Attributes: map[string]schema.Attribute{
			"templates": schema.ListNestedAttribute{
				MarkdownDescription: "Available survey templates.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":   schema.Int64Attribute{Computed: true},
						"name": schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *surveyTemplatesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureDataSourceClient(req, resp)
}

func (d *surveyTemplatesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	templates, err := d.client.ListSurveyTemplates(ctx)
	if err != nil {
		addAPIErrorDiagnostic(&resp.Diagnostics, "Unable to read Wellbeing survey templates", err)
		return
	}

	state := surveyTemplatesModel{Templates: make([]surveyTemplateModel, 0, len(templates))}
	for _, template := range templates {
		state.Templates = append(state.Templates, surveyTemplateModel{
			ID:   types.Int64Value(template.ID),
			Name: types.StringValue(template.Name),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
