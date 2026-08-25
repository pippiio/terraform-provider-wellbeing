package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/techchapter/terraform-provider-wellbeing/internal/wellbeingclient"
)

var (
	_ datasource.DataSource              = &apiCallsDataSource{}
	_ datasource.DataSourceWithConfigure = &apiCallsDataSource{}
)

// NewAPICallsDataSource returns the API calls data source.
func NewAPICallsDataSource() datasource.DataSource {
	return &apiCallsDataSource{}
}

type apiCallsDataSource struct {
	client *wellbeingclient.Client
}

type apiCallsModel struct {
	APICalls []apiCallModel `tfsdk:"api_calls"`
}

type apiCallModel struct {
	APIOperationID   types.String `tfsdk:"api_operation_id"`
	UserID           types.Int64  `tfsdk:"user_id"`
	Username         types.String `tfsdk:"username"`
	CreatedOn        types.String `tfsdk:"created_on"`
	Method           types.String `tfsdk:"method"`
	HTTPStatusCode   types.Int64  `tfsdk:"http_status_code"`
	HTTPStatusReason types.String `tfsdk:"http_status_reason"`
}

func (d *apiCallsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api_calls"
}

func (d *apiCallsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Recent API operations and their processing status. Useful for diagnosing a " +
			"queued import: `http_status_code` is null while the operation is being created, `202` once " +
			"queued and while processing, then `200` on success or a 4xx/5xx on failure.",
		Attributes: map[string]schema.Attribute{
			"api_calls": schema.ListNestedAttribute{
				MarkdownDescription: "Recent API operations, in the order the API returns them.",
				Computed:            true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"api_operation_id":   schema.StringAttribute{Computed: true},
						"user_id":            schema.Int64Attribute{Computed: true},
						"username":           schema.StringAttribute{Computed: true},
						"created_on":         schema.StringAttribute{Computed: true},
						"method":             schema.StringAttribute{Computed: true},
						"http_status_code":   schema.Int64Attribute{Computed: true},
						"http_status_reason": schema.StringAttribute{Computed: true},
					},
				},
			},
		},
	}
}

func (d *apiCallsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureDataSourceClient(req, resp)
}

func (d *apiCallsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	calls, err := d.client.ListAPICalls(ctx)
	if err != nil {
		addAPIErrorDiagnostic(&resp.Diagnostics, "Unable to read Wellbeing API calls", err)
		return
	}

	state := apiCallsModel{APICalls: make([]apiCallModel, 0, len(calls))}
	for _, call := range calls {
		statusCode := types.Int64Null()
		if call.HttpStatusCode.Set {
			statusCode = types.Int64Value(int64(call.HttpStatusCode.Int()))
		}

		state.APICalls = append(state.APICalls, apiCallModel{
			APIOperationID:   types.StringValue(call.ApiOperationID),
			UserID:           types.Int64Value(call.UserID),
			Username:         types.StringValue(call.Username),
			CreatedOn:        types.StringValue(call.CreatedOn),
			Method:           types.StringValue(call.Method),
			HTTPStatusCode:   statusCode,
			HTTPStatusReason: types.StringValue(call.HttpStatusReason),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
