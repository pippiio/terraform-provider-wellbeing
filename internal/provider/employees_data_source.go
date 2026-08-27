package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/techchapter/terraform-provider-wellbeing/internal/wellbeingclient"
)

var (
	_ datasource.DataSource              = &employeesDataSource{}
	_ datasource.DataSourceWithConfigure = &employeesDataSource{}
)

// NewEmployeesDataSource returns the employees data source.
func NewEmployeesDataSource() datasource.DataSource {
	return &employeesDataSource{}
}

type employeesDataSource struct {
	client *wellbeingclient.Client
}

type employeesDataSourceModel struct {
	Employees types.Map `tfsdk:"employees"`
}

func (d *employeesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_employees"
}

func (d *employeesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The company's employees, keyed by `EmployeeID`.\n\n" +
			"~> The Wellbeing API returns only employees that were created through the API. " +
			"Employees created in the Wellbeing portal are not included.",
		Attributes: map[string]schema.Attribute{
			"employees": schema.MapNestedAttribute{
				MarkdownDescription: "Employees keyed by `EmployeeID`.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":                           schema.StringAttribute{Computed: true},
						"name":                         schema.StringAttribute{Computed: true},
						"firstname":                    schema.StringAttribute{Computed: true},
						"lastname":                     schema.StringAttribute{Computed: true},
						"email":                        schema.StringAttribute{Computed: true},
						"active":                       schema.BoolAttribute{Computed: true},
						"gender":                       schema.StringAttribute{Computed: true},
						"phone":                        schema.StringAttribute{Computed: true},
						"job_title":                    schema.StringAttribute{Computed: true},
						"invitation_date":              schema.StringAttribute{Computed: true},
						"dimensions":                   schema.MapAttribute{Computed: true, ElementType: types.StringType},
						"wellbeing_id":                 schema.Int64Attribute{Computed: true},
						"fullname":                     schema.StringAttribute{Computed: true},
						"company_id":                   schema.Int64Attribute{Computed: true},
						"external_id":                  schema.StringAttribute{Computed: true},
						"first_invitation_date":        schema.StringAttribute{Computed: true},
						"locked":                       schema.BoolAttribute{Computed: true},
						"opt_out":                      schema.BoolAttribute{Computed: true},
						"sourced_from_external_system": schema.BoolAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *employeesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureDataSourceClient(req, resp)
}

func (d *employeesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	employees, err := d.client.ListEmployees(ctx)
	if err != nil {
		addAPIErrorDiagnostic(&resp.Diagnostics, "Unable to read Wellbeing employees", err)
		return
	}

	// A data source has no prior config to echo, so fromAPIEmployee reports the
	// API's own view of every field.
	models := make(map[string]employeeModel, len(employees))
	for _, employee := range employees {
		model, diags := fromAPIEmployee(ctx, employee, nil, "")
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		models[employee.EmployeeID] = model
	}

	value, diags := types.MapValueFrom(ctx, types.ObjectType{AttrTypes: employeeAttrTypes()}, models)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &employeesDataSourceModel{Employees: value})...)
}
