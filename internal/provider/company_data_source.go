package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/techchapter/terraform-provider-wellbeing/internal/wellbeingclient"
)

var (
	_ datasource.DataSource              = &companyDataSource{}
	_ datasource.DataSourceWithConfigure = &companyDataSource{}
)

// NewCompanyDataSource returns the company data source.
func NewCompanyDataSource() datasource.DataSource {
	return &companyDataSource{}
}

type companyDataSource struct {
	client *wellbeingclient.Client
}

type companyDataSourceModel struct {
	ID               types.String    `tfsdk:"id"`
	EmployeeCount    types.Int64     `tfsdk:"employee_count"`
	EnabledLanguages []languageModel `tfsdk:"enabled_languages"`
}

func (d *companyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_company"
}

func (d *companyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Summary of the configured company.\n\n" +
			"The Wellbeing API has no single company endpoint, so this data source composes what the " +
			"available endpoints expose: the configured company ID, a count from the employee roster, " +
			"and the enabled languages. It issues one request per underlying endpoint.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The company ID the provider is configured with. Requires no request.",
				Computed:            true,
			},
			"employee_count": schema.Int64Attribute{
				MarkdownDescription: "Number of employees in the company.\n\n" +
					"~> Counts only employees created through the API. Employees created in the " +
					"Wellbeing portal are not returned by the API and are therefore not counted.",
				Computed: true,
			},
			"enabled_languages": schema.ListNestedAttribute{
				MarkdownDescription: "Languages enabled for the company. The numeric `id` is the language " +
					"key used in survey question texts. Also available on its own as the " +
					"`wellbeing_enabled_languages` data source.",
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":    schema.Int64Attribute{MarkdownDescription: "Language ID, for example `1045`.", Computed: true},
						"label": schema.StringAttribute{MarkdownDescription: "Display name, for example `English`.", Computed: true},
						"code":  schema.StringAttribute{MarkdownDescription: "Language code, for example `en`.", Computed: true},
					},
				},
			},
		},
	}
}

func (d *companyDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureDataSourceClient(req, resp)
}

func (d *companyDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	state := companyDataSourceModel{
		ID: types.StringValue(d.client.CompanyID()),
	}

	employees, err := d.client.ListEmployees(ctx)
	if err != nil {
		addAPIErrorDiagnostic(&resp.Diagnostics, "Unable to read Wellbeing employees", err)
		return
	}
	state.EmployeeCount = types.Int64Value(int64(len(employees)))

	languages, err := d.client.ListEnabledLanguages(ctx)
	if err != nil {
		addAPIErrorDiagnostic(&resp.Diagnostics, "Unable to read Wellbeing enabled languages", err)
		return
	}

	state.EnabledLanguages = make([]languageModel, 0, len(languages))
	for _, language := range languages {
		state.EnabledLanguages = append(state.EnabledLanguages, languageModel{
			ID:    types.Int64Value(language.ID),
			Label: types.StringValue(language.Label),
			Code:  types.StringValue(language.Code),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
