package provider

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/techchapter/terraform-provider-wellbeing/internal/wellbeingclient"
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
	Host           types.String `tfsdk:"host"`
	Token          types.String `tfsdk:"token"`
	CompanyID      types.String `tfsdk:"company_id"`
	RequestTimeout types.String `tfsdk:"request_timeout"`
	MaxRetries     types.Int64  `tfsdk:"max_retries"`
}

// Metadata returns the provider type name.
func (p *wellbeingProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "wellbeing"
	resp.Version = p.version
}

// Schema defines the provider-level schema for configuration data.
func (p *wellbeingProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manage HR.ON Wellbeing (previously howdy.care) resources.",
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				Description: "Base URI for the Wellbeing API. Defaults to " + wellbeingclient.DefaultHost +
					". May also be provided via the WELLBEING_HOST environment variable.",
				Optional: true,
			},
			"token": schema.StringAttribute{
				Description: "Bearer token for the Wellbeing API, issued to a user with the HRIntegration role. " +
					"May also be provided via the WELLBEING_TOKEN environment variable.",
				Optional:  true,
				Sensitive: true,
			},
			"company_id": schema.StringAttribute{
				Description: "Company ID for the Wellbeing API. " +
					"May also be provided via the WELLBEING_COMPANY_ID environment variable.",
				Optional: true,
			},
			"request_timeout": schema.StringAttribute{
				Description: "Timeout for a single HTTP request, as a Go duration string. Defaults to 5m. " +
					"A full roster replace carrying thousands of employees needs a generous value.",
				Optional: true,
			},
			"max_retries": schema.Int64Attribute{
				Description: "Number of times a retryable request failure is re-attempted. Defaults to 3.",
				Optional:    true,
			},
		},
	}
}

// Configure prepares a wellbeing API client for data sources and resources.
func (p *wellbeingProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config wellbeingProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	unknownAttributes := map[string]bool{
		"host":            config.Host.IsUnknown(),
		"token":           config.Token.IsUnknown(),
		"company_id":      config.CompanyID.IsUnknown(),
		"request_timeout": config.RequestTimeout.IsUnknown(),
		"max_retries":     config.MaxRetries.IsUnknown(),
	}
	for name, unknown := range unknownAttributes {
		if !unknown {
			continue
		}
		resp.Diagnostics.AddAttributeError(
			path.Root(name),
			"Unknown Wellbeing provider configuration value",
			fmt.Sprintf("The provider cannot create the Wellbeing API client because %q is unknown at plan time. "+
				"Either target apply the source of the value first, or set it statically.", name),
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	// Configuration wins over the environment; the environment wins over defaults.
	host := firstNonEmpty(config.Host.ValueString(), os.Getenv("WELLBEING_HOST"), wellbeingclient.DefaultHost)
	token := firstNonEmpty(config.Token.ValueString(), os.Getenv("WELLBEING_TOKEN"))
	companyID := firstNonEmpty(config.CompanyID.ValueString(), os.Getenv("WELLBEING_COMPANY_ID"))

	if token == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"Missing Wellbeing API token",
			"Set the token attribute or the WELLBEING_TOKEN environment variable. "+
				"Generate one in the Wellbeing portal under Access control, on the user holding the HRIntegration role.",
		)
	}
	if companyID == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("company_id"),
			"Missing Wellbeing company ID",
			"Set the company_id attribute or the WELLBEING_COMPANY_ID environment variable. "+
				"The Wellbeing portal displays it alongside the generated API token.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	options := []wellbeingclient.Option{
		wellbeingclient.WithUserAgent("terraform-provider-wellbeing/" + p.version),
	}

	if !config.RequestTimeout.IsNull() {
		timeout, err := time.ParseDuration(config.RequestTimeout.ValueString())
		if err != nil {
			resp.Diagnostics.AddAttributeError(
				path.Root("request_timeout"),
				"Invalid request_timeout",
				fmt.Sprintf("Could not parse %q as a Go duration (for example \"5m\" or \"90s\"): %s",
					config.RequestTimeout.ValueString(), err),
			)
			return
		}
		options = append(options, wellbeingclient.WithTimeout(timeout))
	}

	if !config.MaxRetries.IsNull() {
		if config.MaxRetries.ValueInt64() < 0 {
			resp.Diagnostics.AddAttributeError(
				path.Root("max_retries"),
				"Invalid max_retries",
				"max_retries must be zero or greater.",
			)
			return
		}
		options = append(options, wellbeingclient.WithMaxRetries(int(config.MaxRetries.ValueInt64())))
	}

	client, err := wellbeingclient.NewClient(host, token, companyID, options...)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to create Wellbeing API client",
			"An unexpected error occurred while constructing the Wellbeing API client.\n\n"+err.Error(),
		)
		return
	}

	resp.DataSourceData = client
	resp.ResourceData = client
}

// firstNonEmpty returns the first value that is not the empty string.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// DataSources defines the data sources implemented in the provider.
func (p *wellbeingProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}

// Resources defines the resources implemented in the provider.
func (p *wellbeingProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{}
}
