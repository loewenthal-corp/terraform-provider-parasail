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
		Description: "Lists Parasail dedicated hardware configurations for a model.",
		Attributes: map[string]dschema.Attribute{
			"engine": dschema.StringAttribute{
				Optional:    true,
				Description: "Inference engine. Defaults to VLLM server-side.",
			},
			"model_name": dschema.StringAttribute{
				Required:    true,
				Description: "Model name or Hugging Face model ID.",
			},
			"model_access_key": dschema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Private model access key, such as a Hugging Face token.",
			},
			"draft_model_name": dschema.StringAttribute{
				Optional:    true,
				Description: "Draft model name for speculative decoding.",
			},
			"draft_model_access_key": dschema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Draft model access key.",
			},
			"model_access_key_from": dschema.Int64Attribute{
				Optional:    true,
				Description: "Existing deployment ID to reuse a stored model access key from.",
			},
			"configs": dschema.ListNestedAttribute{
				Computed:    true,
				Description: "Hardware configurations returned by the API.",
				NestedObject: dschema.NestedAttributeObject{
					Attributes: map[string]dschema.Attribute{
						"device":                    dschema.StringAttribute{Computed: true},
						"count":                     dschema.Int64Attribute{Computed: true},
						"display_name":              dschema.StringAttribute{Computed: true},
						"cost":                      dschema.Float64Attribute{Computed: true},
						"estimated_single_user_tps": dschema.Float64Attribute{Computed: true},
						"estimated_system_tps":      dschema.Float64Attribute{Computed: true},
						"recommended":               dschema.BoolAttribute{Computed: true},
						"limited_context":           dschema.BoolAttribute{Computed: true},
						"available":                 dschema.BoolAttribute{Computed: true},
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
