package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Ensure the implementation satisfies the expected interfaces.
var (
	_ provider.Provider = &wellbeingProvider{}
)

// New is a helper function to simplify provider server and testing implementation.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &wellbeingProvider{
			version: version,
		}
	}
}

// wellbeingProvider is the provider implementation.
type wellbeingProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

type wellbeingProviderModel struct {
	Host      types.String `tfsdk:"host"`
	Token     types.String `tfsdk:"token"`
	CompanyID types.String `tfsdk:"company_id"`
}

// Metadata returns the provider type name.
func (p *wellbeingProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "wellbeing"
	resp.Version = p.version
}

// Schema defines the provider-level schema for configuration data.
func (p *wellbeingProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				Description: "URI for Wellbeing API. May also be provided via WELLBEING_HOST environment variable.",
				Optional:    true,
			},
			"token": schema.StringAttribute{
				Description: "Bearer Token for Wellbeing API. May also be provided via WELLBEING_TOKEN environment variable.",
				Optional:    true,
				Sensitive:   true,
			},
			"company_id": schema.StringAttribute{
				Description: "Company ID for Wellbeing API. May also be provided via WELLBEING_COMPANY_ID environment variable.",
				Optional:    true,
			},
		},
	}
}

// Configure prepares a wellbeing API client for data sources and resources.
func (p *wellbeingProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config wellbeingProviderModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if config.Host.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Unknown Wellbeing API Host",
			"The provider cannot create the Wellbeing API client as there is an unknown configuration value for the Wellbeing API host. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the WELLBEING_HOST environment variable.",
		)
	}

	if config.Token.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"Unknown Wellbeing API token",
			"The provider cannot create the Wellbeing API client as there is an unknown configuration value for the Wellbeing API token. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the WELLBEING_TOKEN environment variable.",
		)
	}

	if config.CompanyID.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("company_id"),
			"Unknown Wellbeing Company ID",
			"The provider cannot create the Wellbeing API client as there is an unknown configuration value for the Wellbeing Company ID. "+
				"Either target apply the source of the value first, set the value statically in the configuration, or use the WELLBEING_COMPANY_ID environment variable.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

	host := os.Getenv("WELLBEING_HOST")
	token := os.Getenv("WELLBEING_TOKEN")
	companyId := os.Getenv("WELLBEING_COMPANY_ID")

	if !config.Host.IsNull() {
		host = config.Host.ValueString()
	}

	if !config.Token.IsNull() {
		token = config.Token.ValueString()
	}

	if !config.CompanyID.IsNull() {
		companyId = config.CompanyID.ValueString()
	}

	if host == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Missing Wellbeing API Host",
			"The provider cannot create the Wellbeing API client as there is a missing or empty value for the Wellbeing API host. "+
				"Set the host value in the configuration or use the WELLBEING_HOST environment variable. "+
				"If either is already set, ensure the value is not empty.",
		)
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"Missing Wellbeing API Token",
			"The provider cannot create the Wellbeing API client as there is a missing or empty value for the Wellbeing API token. "+
				"Set the token value in the configuration or use the WELLBEING_TOKEN environment variable. "+
				"If either is already set, ensure the value is not empty.",
		)
	}
	if companyId == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("company_id"),
			"Missing Wellbeing Company ID",
			"The provider cannot create the Wellbeing API client as there is a missing or empty value for the Wellbeing Company ID. "+
				"Set the token value in the configuration or use the WELLBEING_COMPANY_ID environment variable. "+
				"If either is already set, ensure the value is not empty.",
		)
	}

	if resp.Diagnostics.HasError() {
		return
	}

}

// DataSources defines the data sources implemented in the provider.
func (p *wellbeingProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

// Resources defines the resources implemented in the provider.
func (p *wellbeingProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{}
}
