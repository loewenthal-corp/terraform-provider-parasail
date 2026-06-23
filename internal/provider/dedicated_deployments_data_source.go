package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	parasail "github.com/loewenthal-corp/terraform-provider-parasail/internal/client"
)

type dedicatedDeploymentsDataSource struct {
	api *apiConfig
}

type dedicatedDeploymentsDataSourceModel struct {
	Deployments []deploymentSummaryModel `tfsdk:"deployments"`
}

type deploymentSummaryModel struct {
	ID                    types.String      `tfsdk:"id"`
	Name                  types.String      `tfsdk:"name"`
	DisplayName           types.String      `tfsdk:"display_name"`
	Model                 types.String      `tfsdk:"model"`
	ModelAlias            types.String      `tfsdk:"model_alias"`
	BaseURL               types.String      `tfsdk:"base_url"`
	Engine                types.String      `tfsdk:"engine"`
	Mode                  types.String      `tfsdk:"mode"`
	Replicas              types.Int64       `tfsdk:"replicas"`
	Autoscaling           types.Bool        `tfsdk:"autoscaling"`
	MinReplicas           types.Int64       `tfsdk:"min_replicas"`
	MaxReplicas           types.Int64       `tfsdk:"max_replicas"`
	Status                types.String      `tfsdk:"status"`
	StatusMessage         types.String      `tfsdk:"status_message"`
	CreatedAt             types.Int64       `tfsdk:"created_at"`
	StartedAt             types.Int64       `tfsdk:"started_at"`
	DeploymentAvailableAt types.Int64       `tfsdk:"deployment_available_at"`
	ModifiedAt            types.Int64       `tfsdk:"modified_at"`
	GPUs                  []gpuSummaryModel `tfsdk:"gpu"`
}

type gpuSummaryModel struct {
	Type        types.String `tfsdk:"type"`
	Count       types.Int64  `tfsdk:"count"`
	DisplayName types.String `tfsdk:"display_name"`
	Available   types.Bool   `tfsdk:"available"`
	Selected    types.Bool   `tfsdk:"selected"`
}

var _ datasource.DataSource = &dedicatedDeploymentsDataSource{}
var _ datasource.DataSourceWithConfigure = &dedicatedDeploymentsDataSource{}

func NewDedicatedDeploymentsDataSource() datasource.DataSource {
	return &dedicatedDeploymentsDataSource{}
}

func (d *dedicatedDeploymentsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dedicated_deployments"
}

func (d *dedicatedDeploymentsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
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

func (d *dedicatedDeploymentsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = dschema.Schema{
		Description: "Lists existing Parasail dedicated model deployments visible to the configured API key.",
		Attributes: map[string]dschema.Attribute{
			"deployments": dschema.ListNestedAttribute{
				Computed:    true,
				Description: "Existing dedicated deployments.",
				NestedObject: dschema.NestedAttributeObject{
					Attributes: map[string]dschema.Attribute{
						"id":                      dschema.StringAttribute{Computed: true},
						"name":                    dschema.StringAttribute{Computed: true},
						"display_name":            dschema.StringAttribute{Computed: true},
						"model":                   dschema.StringAttribute{Computed: true},
						"model_alias":             dschema.StringAttribute{Computed: true},
						"base_url":                dschema.StringAttribute{Computed: true},
						"engine":                  dschema.StringAttribute{Computed: true},
						"mode":                    dschema.StringAttribute{Computed: true},
						"replicas":                dschema.Int64Attribute{Computed: true},
						"autoscaling":             dschema.BoolAttribute{Computed: true},
						"min_replicas":            dschema.Int64Attribute{Computed: true},
						"max_replicas":            dschema.Int64Attribute{Computed: true},
						"status":                  dschema.StringAttribute{Computed: true},
						"status_message":          dschema.StringAttribute{Computed: true},
						"created_at":              dschema.Int64Attribute{Computed: true},
						"started_at":              dschema.Int64Attribute{Computed: true},
						"deployment_available_at": dschema.Int64Attribute{Computed: true},
						"modified_at":             dschema.Int64Attribute{Computed: true},
						"gpu": dschema.ListNestedAttribute{
							Computed:    true,
							Description: "GPU profiles associated with the deployment.",
							NestedObject: dschema.NestedAttributeObject{
								Attributes: map[string]dschema.Attribute{
									"type":         dschema.StringAttribute{Computed: true},
									"count":        dschema.Int64Attribute{Computed: true},
									"display_name": dschema.StringAttribute{Computed: true},
									"available":    dschema.BoolAttribute{Computed: true},
									"selected":     dschema.BoolAttribute{Computed: true},
								},
							},
						},
					},
				},
			},
		},
	}
}

func (d *dedicatedDeploymentsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	deployments, err := d.api.Client.ListDeployments(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list dedicated deployments", err.Error())
		return
	}

	state := dedicatedDeploymentsDataSourceModel{
		Deployments: make([]deploymentSummaryModel, 0, len(deployments)),
	}
	for _, deployment := range deployments {
		state.Deployments = append(state.Deployments, flattenDeploymentSummary(deployment))
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func flattenDeploymentSummary(deployment parasail.DedicatedDeployment) deploymentSummaryModel {
	model := deploymentSummaryModel{
		ID:                    types.StringValue(strconv.FormatInt(deployment.ID, 10)),
		Name:                  stringComputed(deployment.DeploymentName),
		DisplayName:           stringComputed(deployment.DisplayName),
		Model:                 stringComputed(deployment.ModelName),
		ModelAlias:            stringComputed(deployment.ExternalAlias),
		BaseURL:               stringComputed(deployment.BaseURL),
		Engine:                stringComputed(deployment.Engine),
		Mode:                  stringComputed(deployment.EngineTask),
		Replicas:              types.Int64Value(deployment.Replicas),
		Autoscaling:           boolComputed(deployment.Autoscaling),
		MinReplicas:           int64Computed(deployment.MinReplicas),
		MaxReplicas:           int64Computed(deployment.MaxReplicas),
		CreatedAt:             int64Computed(deployment.CreatedAt),
		StartedAt:             int64Computed(deployment.StartedAt),
		DeploymentAvailableAt: int64Computed(deployment.DeploymentAvailableAt),
		ModifiedAt:            int64Computed(deployment.ModifiedAt),
		GPUs:                  make([]gpuSummaryModel, 0, len(deployment.DeviceConfigs)),
	}

	if deployment.Status != nil {
		model.Status = stringComputed(deployment.Status.Status)
		model.StatusMessage = stringComputed(deployment.Status.StatusMessage)
	}

	for _, gpu := range deployment.DeviceConfigs {
		model.GPUs = append(model.GPUs, gpuSummaryModel{
			Type:        stringComputed(gpu.Device),
			Count:       types.Int64Value(gpu.Count),
			DisplayName: stringComputed(gpu.DisplayName),
			Available:   types.BoolValue(gpu.Available),
			Selected:    types.BoolValue(gpu.Selected),
		})
	}

	return model
}

func boolComputed(value *bool) types.Bool {
	if value == nil {
		return types.BoolNull()
	}
	return types.BoolValue(*value)
}
