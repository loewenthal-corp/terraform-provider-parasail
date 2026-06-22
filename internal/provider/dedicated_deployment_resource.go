package provider

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	parasail "github.com/loewenthal-corp/terraform-provider-parasail/internal/client"
)

const defaultScaleDownAfter = "8h"

type dedicatedDeploymentResource struct {
	api *apiConfig
}

type dedicatedDeploymentResourceModel struct {
	ID                       types.String      `tfsdk:"id"`
	Name                     types.String      `tfsdk:"name"`
	Model                    types.String      `tfsdk:"model"`
	ModelAccessKey           types.String      `tfsdk:"model_access_key"`
	Description              types.String      `tfsdk:"description"`
	Tags                     types.List        `tfsdk:"tags"`
	GPUs                     []gpuModel        `tfsdk:"gpu"`
	Autoscaling              *autoscalingModel `tfsdk:"autoscaling"`
	Engine                   types.String      `tfsdk:"engine"`
	Mode                     types.String      `tfsdk:"mode"`
	ScaleDownAfter           types.String      `tfsdk:"scale_down_after"`
	DraftModel               types.String      `tfsdk:"draft_model"`
	DraftModelAccessKey      types.String      `tfsdk:"draft_model_access_key"`
	MaxConnectionsPerReplica types.Int64       `tfsdk:"max_connections_per_replica"`
	ContextLength            types.Int64       `tfsdk:"context_length"`
	MaxOutputTokens          types.Int64       `tfsdk:"max_output_tokens"`
	MaxRequestDuration       types.String      `tfsdk:"max_request_duration"`
	ChatTemplate             types.String      `tfsdk:"chat_template"`
	WaitForOnline            types.Bool        `tfsdk:"wait_for_online"`
	ModelAlias               types.String      `tfsdk:"model_alias"`
	BaseURL                  types.String      `tfsdk:"base_url"`
	Status                   types.String      `tfsdk:"status"`
	StatusMessage            types.String      `tfsdk:"status_message"`
	DeploymentAvailableAt    types.Int64       `tfsdk:"deployment_available_at"`
}

type gpuModel struct {
	Type  types.String `tfsdk:"type"`
	Count types.Int64  `tfsdk:"count"`
}

type autoscalingModel struct {
	MinReplicas                 types.Int64   `tfsdk:"min_replicas"`
	MaxReplicas                 types.Int64   `tfsdk:"max_replicas"`
	TargetConnectionsPerReplica types.Int64   `tfsdk:"target_connections_per_replica"`
	SmoothingFactor             types.Float64 `tfsdk:"smoothing_factor"`
}

var _ resource.Resource = &dedicatedDeploymentResource{}
var _ resource.ResourceWithConfigure = &dedicatedDeploymentResource{}

func NewDedicatedDeploymentResource() resource.Resource {
	return &dedicatedDeploymentResource{}
}

func (r *dedicatedDeploymentResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dedicated_deployment"
}

func (r *dedicatedDeploymentResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	api, ok := req.ProviderData.(*apiConfig)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider data", fmt.Sprintf("Expected *apiConfig, got %T", req.ProviderData))
		return
	}

	r.api = api
}

func (r *dedicatedDeploymentResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = rschema.Schema{
		Description: "Manages a Parasail dedicated model deployment.",
		Attributes: map[string]rschema.Attribute{
			"id": rschema.StringAttribute{
				Computed:      true,
				Description:   "Parasail deployment ID.",
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": rschema.StringAttribute{
				Required:    true,
				Description: "Deployment name. This is the stable Terraform-facing name and becomes part of the computed model_alias.",
			},
			"model": rschema.StringAttribute{
				Required:    true,
				Description: "Model name or Hugging Face model ID.",
			},
			"model_access_key": rschema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Private model access key, such as a Hugging Face token.",
			},
			"description": rschema.StringAttribute{
				Optional:    true,
				Description: "Human-readable deployment description.",
			},
			"tags": rschema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Deployment tags.",
			},
			"engine": rschema.StringAttribute{
				Optional:    true,
				Description: "Inference engine. Defaults to VLLM.",
			},
			"mode": rschema.StringAttribute{
				Optional:    true,
				Description: "Model serving mode. Defaults to AUTO. API values include AUTO, GENERATE, EMBED, SCORE, TRANSCRIPTION, SPEECH, TEXT2IMAGE, IMAGE2IMAGE, and INPAINTING.",
			},
			"scale_down_after": rschema.StringAttribute{
				Optional:    true,
				Description: "How long to keep replicas running before scaling down, such as \"8h\" or \"30m\". Use \"never\" to disable scale-down. Defaults to 8h.",
			},
			"draft_model": rschema.StringAttribute{
				Optional:    true,
				Description: "Draft model for speculative decoding.",
			},
			"draft_model_access_key": rschema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Draft model access key.",
			},
			"max_connections_per_replica": rschema.Int64Attribute{
				Optional:    true,
				Description: "Maximum concurrent requests per replica.",
			},
			"context_length": rschema.Int64Attribute{
				Optional:    true,
				Description: "Maximum context window override.",
			},
			"max_output_tokens": rschema.Int64Attribute{
				Optional:    true,
				Description: "Maximum output tokens.",
			},
			"max_request_duration": rschema.StringAttribute{
				Optional:    true,
				Description: "Maximum request duration, such as \"5m\" or \"30s\".",
			},
			"chat_template": rschema.StringAttribute{
				Optional:    true,
				Description: "Chat template override.",
			},
			"wait_for_online": rschema.BoolAttribute{
				Optional:    true,
				Description: "Wait for the deployment status to become ONLINE after create/update. Defaults to true.",
			},
			"model_alias": rschema.StringAttribute{
				Computed:    true,
				Description: "The model identifier to use in OpenAI-compatible inference requests.",
			},
			"base_url": rschema.StringAttribute{
				Computed:    true,
				Description: "Parasail gateway base URL.",
			},
			"status": rschema.StringAttribute{
				Computed:    true,
				Description: "Deployment status.",
			},
			"status_message": rschema.StringAttribute{
				Computed:    true,
				Description: "Deployment status message.",
			},
			"deployment_available_at": rschema.Int64Attribute{
				Computed:    true,
				Description: "Unix epoch milliseconds when the deployment first became available.",
			},
		},
		Blocks: map[string]rschema.Block{
			"gpu": rschema.ListNestedBlock{
				Description: "A GPU configuration to make available to the scheduler. Most deployments should use one gpu block.",
				NestedObject: rschema.NestedBlockObject{
					Attributes: map[string]rschema.Attribute{
						"type": rschema.StringAttribute{
							Required:    true,
							Description: "Device type, as returned by parasail_dedicated_device_configs.",
						},
						"count": rschema.Int64Attribute{
							Required:    true,
							Description: "Number of GPUs for this replica.",
						},
					},
				},
			},
			"autoscaling": rschema.SingleNestedBlock{
				Description: "Autoscaling settings. Omit this block for a fixed-size deployment.",
				Attributes: map[string]rschema.Attribute{
					"min_replicas": rschema.Int64Attribute{
						Optional:    true,
						Description: "Minimum replica count.",
					},
					"max_replicas": rschema.Int64Attribute{
						Required:    true,
						Description: "Maximum replica count.",
					},
					"target_connections_per_replica": rschema.Int64Attribute{
						Optional:    true,
						Description: "Target active requests per replica.",
					},
					"smoothing_factor": rschema.Float64Attribute{
						Optional:    true,
						Description: "Autoscaling load smoothing factor. Defaults to the API behavior if omitted.",
					},
				},
			},
		},
	}
}

func (r *dedicatedDeploymentResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dedicatedDeploymentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	payload, diags := expandDeployment(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.api.Client.CreateDeployment(ctx, payload)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create dedicated deployment", err.Error())
		return
	}

	plan.ID = types.StringValue(strconv.FormatInt(created.ID, 10))
	if shouldWaitForOnline(plan.WaitForOnline) {
		created, err = r.waitForStatus(ctx, created.ID, "ONLINE")
		if err != nil {
			resp.Diagnostics.AddError("Dedicated deployment did not become ONLINE", err.Error())
			return
		}
	}

	flattenDeployment(&plan, created)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dedicatedDeploymentResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dedicatedDeploymentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := parseDeploymentID(state.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid deployment ID", err.Error())
		return
	}

	remote, err := r.api.Client.GetDeployment(ctx, id)
	if parasail.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to read dedicated deployment", err.Error())
		return
	}

	flattenDeployment(&state, remote)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *dedicatedDeploymentResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan dedicatedDeploymentResourceModel
	var state dedicatedDeploymentResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := parseDeploymentID(state.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid deployment ID", err.Error())
		return
	}

	payload, diags := expandDeployment(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updated, err := r.api.Client.UpdateDeployment(ctx, id, payload)
	if err != nil {
		resp.Diagnostics.AddError("Unable to update dedicated deployment", err.Error())
		return
	}

	plan.ID = state.ID
	if shouldWaitForOnline(plan.WaitForOnline) {
		updated, err = r.waitForStatus(ctx, id, "ONLINE")
		if err != nil {
			resp.Diagnostics.AddError("Dedicated deployment did not become ONLINE", err.Error())
			return
		}
	}

	flattenDeployment(&plan, updated)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dedicatedDeploymentResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dedicatedDeploymentResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id, err := parseDeploymentID(state.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid deployment ID", err.Error())
		return
	}

	err = r.api.Client.DeleteDeployment(ctx, id)
	if parasail.IsNotFound(err) {
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Unable to delete dedicated deployment", err.Error())
		return
	}

	if err := r.waitUntilDeleted(ctx, id); err != nil {
		resp.Diagnostics.AddError("Dedicated deployment was not deleted", err.Error())
	}
}

func expandDeployment(ctx context.Context, model dedicatedDeploymentResourceModel) (parasail.DedicatedDeployment, diag.Diagnostics) {
	var diags diag.Diagnostics

	if len(model.GPUs) == 0 {
		diags.AddAttributeError(
			path.Root("gpu"),
			"Missing GPU configuration",
			"At least one gpu block is required.",
		)
		return parasail.DedicatedDeployment{}, diags
	}

	tags := []string{}
	if !model.Tags.IsNull() && !model.Tags.IsUnknown() {
		diags.Append(model.Tags.ElementsAs(ctx, &tags, false)...)
	}

	scaleDownPolicy, scaleDownThreshold, scaleDiags := parseScaleDownAfter(model.ScaleDownAfter)
	diags.Append(scaleDiags...)

	maxRequestDuration, durationDiags := parseOptionalDurationMS(model.MaxRequestDuration, "max_request_duration")
	diags.Append(durationDiags...)

	deviceConfigs := make([]parasail.DedicatedDeviceConfig, 0, len(model.GPUs))
	for _, gpu := range model.GPUs {
		deviceConfigs = append(deviceConfigs, parasail.DedicatedDeviceConfig{
			Device:   gpu.Type.ValueString(),
			Count:    gpu.Count.ValueInt64(),
			Selected: true,
		})
	}

	deployment := parasail.DedicatedDeployment{
		DeploymentName:           model.Name.ValueString(),
		ModelName:                model.Model.ValueString(),
		ModelAccessKey:           stringValue(model.ModelAccessKey, ""),
		Description:              stringValue(model.Description, ""),
		Tags:                     tags,
		Engine:                   stringValue(model.Engine, "VLLM"),
		EngineTask:               stringValue(model.Mode, "AUTO"),
		ScaleDownPolicy:          scaleDownPolicy,
		ScaleDownThreshold:       scaleDownThreshold,
		DraftModelName:           stringValue(model.DraftModel, ""),
		DraftModelAccessKey:      stringValue(model.DraftModelAccessKey, ""),
		MaxConnectionsPerReplica: int64Pointer(model.MaxConnectionsPerReplica),
		ContextLengthOverride:    int64Pointer(model.ContextLength),
		MaxCompletionTokens:      int64Pointer(model.MaxOutputTokens),
		MaxRequestDuration:       maxRequestDuration,
		ChatTemplateOverride:     stringValue(model.ChatTemplate, ""),
		DeviceConfigs:            deviceConfigs,
	}

	if model.Autoscaling != nil {
		enabled := true
		deployment.Autoscaling = &enabled
		deployment.Replicas = int64Value(model.Autoscaling.MinReplicas, 1)
		deployment.MinReplicas = int64PointerDefault(model.Autoscaling.MinReplicas, 1)
		deployment.MaxReplicas = int64Pointer(model.Autoscaling.MaxReplicas)
		deployment.TargetConnections = int64Pointer(model.Autoscaling.TargetConnectionsPerReplica)
		deployment.AutoscalingFactor = float64Pointer(model.Autoscaling.SmoothingFactor)
	} else {
		enabled := false
		deployment.Autoscaling = &enabled
		deployment.Replicas = 1
	}

	return deployment, diags
}

func flattenDeployment(model *dedicatedDeploymentResourceModel, deployment *parasail.DedicatedDeployment) {
	model.ID = types.StringValue(strconv.FormatInt(deployment.ID, 10))
	model.Name = types.StringValue(deployment.DeploymentName)
	model.Model = types.StringValue(deployment.ModelName)
	model.ModelAlias = stringComputed(deployment.ExternalAlias)
	model.BaseURL = stringComputed(deployment.BaseURL)
	model.DeploymentAvailableAt = int64Computed(deployment.DeploymentAvailableAt)
	if deployment.ScaleDownPolicy != "" {
		model.ScaleDownAfter = types.StringValue(formatScaleDownAfter(deployment.ScaleDownPolicy, deployment.ScaleDownThreshold))
	}
	if deployment.Status != nil {
		model.Status = stringComputed(deployment.Status.Status)
		model.StatusMessage = stringComputed(deployment.Status.StatusMessage)
	}
}

func (r *dedicatedDeploymentResource) waitForStatus(ctx context.Context, id int64, target string) (*parasail.DedicatedDeployment, error) {
	ctx, cancel := context.WithTimeout(ctx, r.api.OperationTimeout)
	defer cancel()

	ticker := time.NewTicker(r.api.PollInterval)
	defer ticker.Stop()

	for {
		deployment, err := r.api.Client.GetDeployment(ctx, id)
		if err != nil {
			return nil, err
		}
		if deployment.Status != nil {
			switch deployment.Status.Status {
			case target:
				return deployment, nil
			case "ERROR":
				return nil, fmt.Errorf("deployment entered ERROR: %s", deployment.Status.StatusMessage)
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func (r *dedicatedDeploymentResource) waitUntilDeleted(ctx context.Context, id int64) error {
	ctx, cancel := context.WithTimeout(ctx, r.api.OperationTimeout)
	defer cancel()

	ticker := time.NewTicker(r.api.PollInterval)
	defer ticker.Stop()

	for {
		deployments, err := r.api.Client.ListDeployments(ctx)
		if err != nil {
			return err
		}

		found := false
		for _, deployment := range deployments {
			if deployment.ID == id {
				found = true
				break
			}
		}
		if !found {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func parseScaleDownAfter(value types.String) (string, int64, diag.Diagnostics) {
	var diags diag.Diagnostics
	raw := defaultScaleDownAfter
	if !value.IsNull() && !value.IsUnknown() {
		raw = value.ValueString()
	}

	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "never", "none", "0", "0s":
		return "NONE", 0, diags
	}

	duration, err := time.ParseDuration(raw)
	if err != nil {
		diags.AddAttributeError(
			path.Root("scale_down_after"),
			"Invalid scale_down_after",
			"scale_down_after must be a Go-style duration such as \"8h\", \"30m\", or \"45s\", or \"never\".",
		)
		return "", 0, diags
	}
	return "TIMER", duration.Milliseconds(), diags
}

func formatScaleDownAfter(policy string, thresholdMS int64) string {
	if strings.EqualFold(policy, "NONE") {
		return "never"
	}
	duration := time.Duration(thresholdMS) * time.Millisecond
	if duration%time.Hour == 0 {
		return fmt.Sprintf("%dh", int64(duration/time.Hour))
	}
	if duration%time.Minute == 0 {
		return fmt.Sprintf("%dm", int64(duration/time.Minute))
	}
	if duration%time.Second == 0 {
		return fmt.Sprintf("%ds", int64(duration/time.Second))
	}
	return duration.String()
}

func parseOptionalDurationMS(value types.String, attribute string) (*int64, diag.Diagnostics) {
	var diags diag.Diagnostics
	if value.IsNull() || value.IsUnknown() || value.ValueString() == "" {
		return nil, diags
	}

	duration, err := time.ParseDuration(value.ValueString())
	if err != nil {
		diags.AddAttributeError(
			path.Root(attribute),
			"Invalid "+attribute,
			attribute+" must be a Go-style duration such as \"5m\", \"30s\", or \"1500ms\".",
		)
		return nil, diags
	}

	ms := duration.Milliseconds()
	return &ms, diags
}

func parseDeploymentID(id types.String) (int64, error) {
	if id.IsNull() || id.IsUnknown() {
		return 0, fmt.Errorf("missing deployment ID")
	}
	return strconv.ParseInt(id.ValueString(), 10, 64)
}

func shouldWaitForOnline(value types.Bool) bool {
	return value.IsNull() || value.IsUnknown() || value.ValueBool()
}

func stringValue(value types.String, fallback string) string {
	if value.IsNull() || value.IsUnknown() {
		return fallback
	}
	return value.ValueString()
}

func int64Value(value types.Int64, fallback int64) int64 {
	if value.IsNull() || value.IsUnknown() {
		return fallback
	}
	return value.ValueInt64()
}

func int64Pointer(value types.Int64) *int64 {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	v := value.ValueInt64()
	return &v
}

func int64PointerDefault(value types.Int64, fallback int64) *int64 {
	v := int64Value(value, fallback)
	return &v
}

func float64Pointer(value types.Float64) *float64 {
	if value.IsNull() || value.IsUnknown() {
		return nil
	}
	v := value.ValueFloat64()
	return &v
}

func stringComputed(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}

func int64Computed(value *int64) types.Int64 {
	if value == nil {
		return types.Int64Null()
	}
	return types.Int64Value(*value)
}
