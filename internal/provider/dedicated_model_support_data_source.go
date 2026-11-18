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
		MarkdownDescription: "Checks whether Parasail can serve a model as a dedicated deployment, and which engines support it.",
		Attributes: map[string]dschema.Attribute{
			"model_name": dschema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Hugging Face model ID, such as `Qwen/Qwen2.5-7B-Instruct`.",
			},
			"model_access_key": dschema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Access key for a private or gated model, such as a Hugging Face token.",
			},
			"model_access_key_from": dschema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "ID of an existing deployment whose stored model access key Parasail should reuse instead of `model_access_key`.",
			},
			"engine": dschema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Inference engine to check, such as `VLLM`. If omitted, Parasail checks its default engine.",
			},
			"supported": dschema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the model can be deployed with `engine`.",
			},
			"known": dschema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether Parasail recognizes the model, for example because it exists on Hugging Face and the access key can read it.",
			},
			"supporting_engines": dschema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Engines that can serve the model.",
			},
			"messages": dschema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Validation messages from Parasail explaining the result.",
				NestedObject: dschema.NestedAttributeObject{
					Attributes: map[string]dschema.Attribute{
						"level": dschema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Message severity, such as `INFO`, `WARN`, or `ERROR`.",
						},
						"content": dschema.StringAttribute{
							Computed:            true,
							MarkdownDescription: "Message text.",
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
