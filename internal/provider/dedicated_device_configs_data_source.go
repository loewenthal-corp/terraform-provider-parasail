package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	parasail "github.com/loewenthal-corp/terraform-provider-parasail/internal/client"
)

type dedicatedDeviceConfigsDataSource struct {
	api *apiConfig
}

type dedicatedDeviceConfigsDataSourceModel struct {
	Engine              types.String                  `tfsdk:"engine"`
	ModelName           types.String                  `tfsdk:"model_name"`
	ModelAccessKey      types.String                  `tfsdk:"model_access_key"`
	DraftModelName      types.String                  `tfsdk:"draft_model_name"`
	DraftModelAccessKey types.String                  `tfsdk:"draft_model_access_key"`
	ModelAccessKeyFrom  types.Int64                   `tfsdk:"model_access_key_from"`
	Configs             []deviceConfigDataSourceModel `tfsdk:"configs"`
}

type deviceConfigDataSourceModel struct {
	Device                 types.String  `tfsdk:"device"`
	Count                  types.Int64   `tfsdk:"count"`
	DisplayName            types.String  `tfsdk:"display_name"`
	Cost                   types.Float64 `tfsdk:"cost"`
	EstimatedSingleUserTPS types.Float64 `tfsdk:"estimated_single_user_tps"`
	EstimatedSystemTPS     types.Float64 `tfsdk:"estimated_system_tps"`
	Recommended            types.Bool    `tfsdk:"recommended"`
	LimitedContext         types.Bool    `tfsdk:"limited_context"`
	Available              types.Bool    `tfsdk:"available"`
}

var _ datasource.DataSource = &dedicatedDeviceConfigsDataSource{}
var _ datasource.DataSourceWithConfigure = &dedicatedDeviceConfigsDataSource{}

func NewDedicatedDeviceConfigsDataSource() datasource.DataSource {
	return &dedicatedDeviceConfigsDataSource{}
}

func (d *dedicatedDeviceConfigsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dedicated_device_configs"
}

func (d *dedicatedDeviceConfigsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *dedicatedDeviceConfigsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		MarkdownDescription: "Lists the GPU configurations Parasail offers for deploying a model, with price and availability. Use it to choose `gpu` blocks for `parasail_dedicated_deployment`. Configurations are returned in Parasail's order, roughly cheapest first.",
		Attributes: map[string]dschema.Attribute{
			"engine": dschema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Inference engine, such as `VLLM`. If omitted, Parasail uses its default engine.",
			},
			"model_name": dschema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Hugging Face model ID, such as `Qwen/Qwen2.5-7B-Instruct`.",
			},
			"model_access_key": dschema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Access key for a private or gated model, such as a Hugging Face token.",
			},
			"draft_model_name": dschema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Hugging Face model ID of a draft model for speculative decoding. Configurations then account for both models.",
			},
			"draft_model_access_key": dschema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Access key for a private or gated `draft_model_name`.",
			},
			"model_access_key_from": dschema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "ID of an existing deployment whose stored model access key Parasail should reuse instead of `model_access_key`.",
			},
			"configs": dschema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "GPU configurations offered for the model.",
				NestedObject: dschema.NestedAttributeObject{
					Attributes: map[string]dschema.Attribute{
						"device":                    dschema.StringAttribute{Computed: true, MarkdownDescription: "GPU type. Use as `gpu.type` on `parasail_dedicated_deployment`."},
						"count":                     dschema.Int64Attribute{Computed: true, MarkdownDescription: "GPUs per replica. Use as `gpu.count` on `parasail_dedicated_deployment`."},
						"display_name":              dschema.StringAttribute{Computed: true, MarkdownDescription: "Human-readable configuration name, such as `H100SXM x 2`."},
						"cost":                      dschema.Float64Attribute{Computed: true, MarkdownDescription: "Price per replica-hour in US dollars, as quoted by Parasail."},
						"estimated_single_user_tps": dschema.Float64Attribute{Computed: true, MarkdownDescription: "Estimated tokens per second for a single request, when Parasail provides one."},
						"estimated_system_tps":      dschema.Float64Attribute{Computed: true, MarkdownDescription: "Estimated aggregate tokens per second per replica, when Parasail provides one."},
						"recommended":               dschema.BoolAttribute{Computed: true, MarkdownDescription: "Whether Parasail recommends this configuration for the model."},
						"limited_context":           dschema.BoolAttribute{Computed: true, MarkdownDescription: "Whether this configuration cannot fit the model's full context window."},
						"available":                 dschema.BoolAttribute{Computed: true, MarkdownDescription: "Whether Parasail currently has capacity for this configuration."},
					},
				},
			},
		},
	}
}

func (d *dedicatedDeviceConfigsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config dedicatedDeviceConfigsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	configs, err := d.api.Client.DeviceConfigs(ctx, parasail.DeviceConfigsRequest{
		Engine:              stringValue(config.Engine, ""),
		ModelName:           config.ModelName.ValueString(),
		ModelAccessKey:      stringValue(config.ModelAccessKey, ""),
		DraftModelName:      stringValue(config.DraftModelName, ""),
		DraftModelAccessKey: stringValue(config.DraftModelAccessKey, ""),
		ModelAccessKeyFrom:  int64Value(config.ModelAccessKeyFrom, 0),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to list dedicated device configs", err.Error())
		return
	}

	config.Configs = make([]deviceConfigDataSourceModel, 0, len(configs))
	for _, item := range configs {
		config.Configs = append(config.Configs, deviceConfigDataSourceModel{
			Device:                 stringComputed(item.Device),
			Count:                  types.Int64Value(item.Count),
			DisplayName:            stringComputed(item.DisplayName),
			Cost:                   float64Computed(item.Cost),
			EstimatedSingleUserTPS: float64Computed(item.EstimatedSingleUserTPS),
			EstimatedSystemTPS:     float64Computed(item.EstimatedSystemTPS),
			Recommended:            types.BoolValue(item.Recommended),
			LimitedContext:         types.BoolValue(item.LimitedContext),
			Available:              types.BoolValue(item.Available),
		})
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}

func float64Computed(value *float64) types.Float64 {
	if value == nil {
		return types.Float64Null()
	}
	return types.Float64Value(*value)
}
