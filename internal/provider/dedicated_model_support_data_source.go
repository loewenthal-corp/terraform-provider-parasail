package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	parasail "github.com/loewenthal-corp/terraform-provider-parasail/internal/client"
)

type dedicatedModelSupportDataSource struct {
	api *apiConfig
}

type dedicatedModelSupportDataSourceModel struct {
	ModelName          types.String      `tfsdk:"model_name"`
	ModelAccessKey     types.String      `tfsdk:"model_access_key"`
	ModelAccessKeyFrom types.Int64       `tfsdk:"model_access_key_from"`
	Engine             types.String      `tfsdk:"engine"`
	Supported          types.Bool        `tfsdk:"supported"`
	Known              types.Bool        `tfsdk:"known"`
	SupportingEngines  types.List        `tfsdk:"supporting_engines"`
	Messages           []logMessageModel `tfsdk:"messages"`
}

type logMessageModel struct {
	Level   types.String `tfsdk:"level"`
	Content types.String `tfsdk:"content"`
}

var _ datasource.DataSource = &dedicatedModelSupportDataSource{}
var _ datasource.DataSourceWithConfigure = &dedicatedModelSupportDataSource{}

func NewDedicatedModelSupportDataSource() datasource.DataSource {
	return &dedicatedModelSupportDataSource{}
}

func (d *dedicatedModelSupportDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dedicated_model_support"
}

func (d *dedicatedModelSupportDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	api, ok := req.ProviderData.(*apiConfig)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *apiConfig, got %T", req.ProviderData))
		return
	}

	d.api = api
}

func (d *dedicatedModelSupportDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "Checks whether Parasail supports a model for dedicated deployment.",
		Attributes: map[string]dschema.Attribute{
			"model_name": dschema.StringAttribute{
				Required:    true,
				Description: "Model name or Hugging Face model ID.",
			},
			"model_access_key": dschema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Private model access key, such as a Hugging Face token.",
			},
			"model_access_key_from": dschema.Int64Attribute{
				Optional:    true,
				Description: "Existing deployment ID to reuse a stored model access key from.",
			},
			"engine": dschema.StringAttribute{
				Optional:    true,
				Description: "Inference engine to check.",
			},
			"supported": dschema.BoolAttribute{
				Computed:    true,
				Description: "Whether the model is supported.",
			},
			"known": dschema.BoolAttribute{
				Computed:    true,
				Description: "Whether Parasail recognizes the model.",
			},
			"supporting_engines": dschema.ListAttribute{
				Computed:    true,
				ElementType: types.StringType,
				Description: "Engines that support this model.",
			},
			"messages": dschema.ListNestedAttribute{
				Computed:    true,
				Description: "Validation messages returned by the API.",
				NestedObject: dschema.NestedAttributeObject{
					Attributes: map[string]dschema.Attribute{
						"level": dschema.StringAttribute{
							Computed: true,
						},
						"content": dschema.StringAttribute{
							Computed: true,
						},
					},
				},
			},
		},
	}
}

func (d *dedicatedModelSupportDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config dedicatedModelSupportDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	support, err := d.api.Client.ModelSupport(ctx, parasail.ModelSupportRequest{
		Engine:             stringValue(config.Engine, ""),
		ModelName:          config.ModelName.ValueString(),
		ModelAccessKey:     stringValue(config.ModelAccessKey, ""),
		ModelAccessKeyFrom: int64Value(config.ModelAccessKeyFrom, 0),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to check model support", err.Error())
		return
	}

	config.Supported = types.BoolValue(support.Supported)
	config.Known = types.BoolValue(support.Known)

	supportingEngines, diags := types.ListValueFrom(ctx, types.StringType, support.SupportingEngines)
	resp.Diagnostics.Append(diags...)
	config.SupportingEngines = supportingEngines

	config.Messages = make([]logMessageModel, 0, len(support.Messages))
	for _, message := range support.Messages {
		config.Messages = append(config.Messages, logMessageModel{
			Level:   stringComputed(message.Level),
			Content: stringComputed(message.Content),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
