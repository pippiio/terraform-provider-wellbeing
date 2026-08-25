package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/techchapter/terraform-provider-wellbeing/internal/wellbeingclient"
)

var (
	_ datasource.DataSource              = &enabledLanguagesDataSource{}
	_ datasource.DataSourceWithConfigure = &enabledLanguagesDataSource{}
)

// NewEnabledLanguagesDataSource returns the enabled languages data source.
func NewEnabledLanguagesDataSource() datasource.DataSource {
	return &enabledLanguagesDataSource{}
}

type enabledLanguagesDataSource struct {
	client *wellbeingclient.Client
}

type enabledLanguagesModel struct {
	Languages []languageModel `tfsdk:"languages"`
}

type languageModel struct {
	ID    types.Int64  `tfsdk:"id"`
	Label types.String `tfsdk:"label"`
	Code  types.String `tfsdk:"code"`
}

func (d *enabledLanguagesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_enabled_languages"
}

func (d *enabledLanguagesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Languages enabled for the company. The numeric `id` is the language key used " +
			"in survey question texts.",
		Attributes: map[string]schema.Attribute{
			"languages": schema.ListNestedAttribute{
				MarkdownDescription: "Enabled languages.",
				Computed:            true,
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

func (d *enabledLanguagesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = configureDataSourceClient(req, resp)
}

func (d *enabledLanguagesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	languages, err := d.client.ListEnabledLanguages(ctx)
	if err != nil {
		addAPIErrorDiagnostic(&resp.Diagnostics, "Unable to read Wellbeing enabled languages", err)
		return
	}

	state := enabledLanguagesModel{Languages: make([]languageModel, 0, len(languages))}
	for _, language := range languages {
		state.Languages = append(state.Languages, languageModel{
			ID:    types.Int64Value(language.ID),
			Label: types.StringValue(language.Label),
			Code:  types.StringValue(language.Code),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
