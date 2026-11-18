package provider

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	parasail "github.com/loewenthal-corp/terraform-provider-parasail/internal/client"
)

const (
	defaultEngine         = "VLLM"
	defaultMode           = "AUTO"
	defaultScaleDownAfter = "8h"
)

var dedicatedDeploymentNameRegexp = regexp.MustCompile(`^[a-z0-9-]+$`)

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
var _ resource.ResourceWithImportState = &dedicatedDeploymentResource{}

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
	useStateInt64 := []planmodifier.Int64{int64planmodifier.UseStateForUnknown()}

	resp.Schema = rschema.Schema{
		MarkdownDescription: "Manages a Parasail dedicated model deployment: a model served on GPUs reserved for your account " +
			"and exposed through Parasail's OpenAI-compatible API under `model_alias`.\n\n" +
			"Use `parasail_dedicated_model_support` to check that a model can be deployed and " +
			"`parasail_dedicated_device_configs` to list the GPU types and counts Parasail offers for it.",
		Attributes: map[string]rschema.Attribute{
			"id": rschema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Numeric Parasail deployment ID, used for import.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": rschema.StringAttribute{
				Required: true,
				MarkdownDescription: "Deployment name. Must be 2-32 characters of lowercase letters, numbers, and hyphens " +
					"(`^[a-z0-9-]+$`). Parasail prefixes it with your account name to build `model_alias`.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(2, 32),
					stringvalidator.RegexMatches(
						dedicatedDeploymentNameRegexp,
						"must contain only lowercase letters, numbers, and hyphens",
					),
				},
			},
			"model": rschema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Hugging Face model ID to serve, such as `Qwen/Qwen2.5-7B-Instruct`.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"model_access_key": rschema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "Access key for a private or gated model, such as a Hugging Face token. " +
					"Parasail does not return it, so it is not populated on import.",
			},
			"description": rschema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Human-readable deployment description.",
			},
			"tags": rschema.ListAttribute{
				Optional:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Free-form tags attached to the deployment.",
			},
			"engine": rschema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(defaultEngine),
				MarkdownDescription: "Inference engine. Defaults to `VLLM`. Use `parasail_dedicated_model_support` to list the engines that support a model.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"mode": rschema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(defaultMode),
				MarkdownDescription: "Model serving mode. Defaults to `AUTO`, which lets Parasail infer the task from the model. " +
					"Other API values include `GENERATE`, `EMBED`, `SCORE`, `TRANSCRIPTION`, `SPEECH`, `TEXT2IMAGE`, " +
					"`IMAGE2IMAGE`, and `INPAINTING`. When `AUTO` is configured, the task Parasail resolves is not treated as drift.",
				Validators: []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"scale_down_after": rschema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(defaultScaleDownAfter),
				MarkdownDescription: "How long an idle deployment keeps running before Parasail scales it down, as a Go duration " +
					"such as `8h`, `30m`, or `90s`. Use `never` to disable scale-down. Defaults to `8h`.",
				Validators: []validator.String{scaleDownAfterValidator{}},
			},
			"draft_model": rschema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Hugging Face model ID of a draft model for speculative decoding.",
			},
			"draft_model_access_key": rschema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "Access key for a private or gated `draft_model`. " +
					"Parasail does not return it, so it is not populated on import.",
			},
			"max_connections_per_replica": rschema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Maximum concurrent requests per replica. Must be at least 1. If omitted, Parasail's default applies.",
				Validators:          []validator.Int64{int64validator.AtLeast(1)},
				PlanModifiers:       useStateInt64,
			},
			"context_length": rschema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Context window override in tokens. Must be at least 1. If omitted, the model's native context length is used.",
				Validators:          []validator.Int64{int64validator.AtLeast(1)},
				PlanModifiers:       useStateInt64,
			},
			"max_output_tokens": rschema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Maximum completion tokens per request. Must be at least 1. If omitted, Parasail's default applies.",
				Validators:          []validator.Int64{int64validator.AtLeast(1)},
				PlanModifiers:       useStateInt64,
			},
			"max_request_duration": rschema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Maximum request duration as a positive Go duration, such as `5m`, `30s`, or `1500ms`. If omitted, Parasail's default applies.",
				Validators:          []validator.String{positiveDurationValidator{}},
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"chat_template": rschema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Jinja chat template that overrides the model's default.",
			},
			"wait_for_online": rschema.BoolAttribute{
				Optional: true,
				MarkdownDescription: "Wait for the deployment status to become `ONLINE` after create and update, bounded by the provider's " +
					"`operation_timeout_minutes`. Defaults to `true`. Set to `false` to return as soon as Parasail accepts the change. " +
					"This is a provider-side setting and is not populated on import.",
			},
			"model_alias": rschema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Model identifier to send as `model` in OpenAI-compatible requests to `base_url`.",
			},
			"base_url": rschema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "OpenAI-compatible base URL for inference requests, such as `https://api.parasail.io/v1`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"status": rschema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Deployment status reported by Parasail, such as `STARTING`, `ONLINE`, `PAUSED`, or `ERROR`.",
			},
			"status_message": rschema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "Detail for `status`, when Parasail provides one.",
			},
			"deployment_available_at": rschema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Unix epoch milliseconds when the deployment first became available. Null until it has been online.",
			},
		},
		Blocks: map[string]rschema.Block{
			"gpu": rschema.ListNestedBlock{
				MarkdownDescription: "A GPU type and count Parasail may use for each replica. At least one block is required. " +
					"Repeat the block to offer alternatives: Parasail provisions on any listed configuration that has capacity. " +
					"Valid combinations come from `parasail_dedicated_device_configs`.",
				Validators: []validator.List{
					listvalidator.IsRequired(),
					listvalidator.SizeAtLeast(1),
				},
				NestedObject: rschema.NestedBlockObject{
					Attributes: map[string]rschema.Attribute{
						"type": rschema.StringAttribute{
							Required:            true,
							MarkdownDescription: "GPU type, such as `RTX5090`, `H100SXM`, or `B200SXM`. Matches `configs[*].device` in `parasail_dedicated_device_configs`.",
							Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
						},
						"count": rschema.Int64Attribute{
							Required:            true,
							MarkdownDescription: "Number of GPUs per replica. Must be at least 1.",
							Validators:          []validator.Int64{int64validator.AtLeast(1)},
						},
					},
				},
			},
			"autoscaling": rschema.SingleNestedBlock{
				MarkdownDescription: "Enables autoscaling. Omit this block for a fixed-size deployment with one replica.",
				Attributes: map[string]rschema.Attribute{
					"min_replicas": rschema.Int64Attribute{
						Optional:            true,
						Computed:            true,
						MarkdownDescription: "Minimum replica count. Defaults to `1`.",
						Validators:          []validator.Int64{int64validator.AtLeast(0)},
						PlanModifiers:       useStateInt64,
					},
					"max_replicas": rschema.Int64Attribute{
						Optional:            true,
						Computed:            true,
						MarkdownDescription: "Maximum replica count. Must be at least 1. If omitted, Parasail's default applies.",
						Validators:          []validator.Int64{int64validator.AtLeast(1)},
						PlanModifiers:       useStateInt64,
					},
					"target_connections_per_replica": rschema.Int64Attribute{
						Optional:            true,
						Computed:            true,
						MarkdownDescription: "Target concurrent requests per replica that triggers scaling. Must be at least 1. If omitted, Parasail's default applies.",
						Validators:          []validator.Int64{int64validator.AtLeast(1)},
						PlanModifiers:       useStateInt64,
					},
					"smoothing_factor": rschema.Float64Attribute{
						Optional:            true,
						Computed:            true,
						MarkdownDescription: "Load smoothing factor for scaling decisions, such as `0.3`. Lower values react more slowly. If omitted, Parasail's default applies.",
						PlanModifiers:       []planmodifier.Float64{float64planmodifier.UseStateForUnknown()},
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

	// Persist the deployment before waiting so a failed or timed-out wait
	// leaves a tainted resource in state instead of an untracked deployment.
	resp.Diagnostics.Append(flattenDeployment(ctx, &plan, created)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() || !shouldWaitForOnline(plan.WaitForOnline) {
		return
	}

	online, err := r.waitForStatus(ctx, created.ID, "ONLINE")
	if err != nil {
		resp.Diagnostics.AddError("Dedicated deployment did not become ONLINE", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenDeployment(ctx, &plan, online)...)
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

	resp.Diagnostics.Append(flattenDeployment(ctx, &state, remote)...)
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
	resp.Diagnostics.Append(flattenDeployment(ctx, &plan, updated)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() || !shouldWaitForOnline(plan.WaitForOnline) {
		return
	}

	online, err := r.waitForStatus(ctx, id, "ONLINE")
	if err != nil {
		resp.Diagnostics.AddError("Dedicated deployment did not become ONLINE", err.Error())
		return
	}

	resp.Diagnostics.Append(flattenDeployment(ctx, &plan, online)...)
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

func (r *dedicatedDeploymentResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if _, err := strconv.ParseInt(req.ID, 10, 64); err != nil {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected a numeric Parasail deployment ID, got %q.", req.ID),
		)
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
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
		Engine:                   stringValue(model.Engine, defaultEngine),
		EngineTask:               stringValue(model.Mode, defaultMode),
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

// flattenDeployment copies the API representation into model. The incoming
// model is the prior plan or state, and it is used to keep the user's
// spelling of values Parasail normalizes (durations, GPU order, letter case,
// AUTO mode) so that Terraform does not report spurious drift.
func flattenDeployment(ctx context.Context, model *dedicatedDeploymentResourceModel, deployment *parasail.DedicatedDeployment) diag.Diagnostics {
	var diags diag.Diagnostics

	model.ID = types.StringValue(strconv.FormatInt(deployment.ID, 10))
	if deployment.DeploymentName != "" {
		model.Name = types.StringValue(deployment.DeploymentName)
	}
	model.Model = preferPriorFold(model.Model, deployment.ModelName)
	model.Engine = preferPriorFold(model.Engine, deployment.Engine)
	model.Mode = flattenMode(model.Mode, deployment.EngineTask)
	model.Description = flattenOptionalString(model.Description, deployment.Description)
	model.DraftModel = flattenOptionalString(model.DraftModel, deployment.DraftModelName)
	model.ChatTemplate = flattenOptionalString(model.ChatTemplate, deployment.ChatTemplateOverride)

	tags, tagDiags := flattenTags(ctx, model.Tags, deployment.Tags)
	diags.Append(tagDiags...)
	model.Tags = tags

	if deployment.ScaleDownPolicy != "" {
		model.ScaleDownAfter = flattenScaleDownAfter(model.ScaleDownAfter, deployment.ScaleDownPolicy, deployment.ScaleDownThreshold)
	}

	// Parasail does not echo every optional setting. When it omits one, keep
	// the value Terraform already knows instead of reporting drift.
	model.MaxConnectionsPerReplica = int64PreferRemote(model.MaxConnectionsPerReplica, deployment.MaxConnectionsPerReplica)
	model.ContextLength = int64PreferRemote(model.ContextLength, deployment.ContextLengthOverride)
	model.MaxOutputTokens = int64PreferRemote(model.MaxOutputTokens, deployment.MaxCompletionTokens)
	model.MaxRequestDuration = flattenDurationMS(model.MaxRequestDuration, deployment.MaxRequestDuration)

	if len(deployment.DeviceConfigs) > 0 {
		model.GPUs = flattenGPUs(model.GPUs, deployment.DeviceConfigs)
	}

	if deployment.Autoscaling != nil {
		model.Autoscaling = flattenAutoscaling(model.Autoscaling, deployment)
	}

	model.ModelAlias = stringComputed(deployment.ExternalAlias)
	model.BaseURL = stringComputed(deployment.BaseURL)
	model.DeploymentAvailableAt = int64Computed(deployment.DeploymentAvailableAt)
	if deployment.Status != nil {
		model.Status = stringComputed(deployment.Status.Status)
		model.StatusMessage = stringComputed(deployment.Status.StatusMessage)
	} else {
		model.Status = nullIfUnknownString(model.Status)
		model.StatusMessage = nullIfUnknownString(model.StatusMessage)
	}

	// Computed values must be known after apply.
	model.MaxConnectionsPerReplica = nullIfUnknownInt64(model.MaxConnectionsPerReplica)
	model.ContextLength = nullIfUnknownInt64(model.ContextLength)
	model.MaxOutputTokens = nullIfUnknownInt64(model.MaxOutputTokens)
	model.MaxRequestDuration = nullIfUnknownString(model.MaxRequestDuration)
	model.ScaleDownAfter = nullIfUnknownString(model.ScaleDownAfter)
	if model.Autoscaling != nil {
		model.Autoscaling.MinReplicas = nullIfUnknownInt64(model.Autoscaling.MinReplicas)
		model.Autoscaling.MaxReplicas = nullIfUnknownInt64(model.Autoscaling.MaxReplicas)
		model.Autoscaling.TargetConnectionsPerReplica = nullIfUnknownInt64(model.Autoscaling.TargetConnectionsPerReplica)
		if model.Autoscaling.SmoothingFactor.IsUnknown() {
			model.Autoscaling.SmoothingFactor = types.Float64Null()
		}
	}

	return diags
}

func flattenMode(prior types.String, remote string) types.String {
	if remote == "" {
		return prior
	}
	// AUTO is resolved server-side to a concrete task; keep AUTO in state.
	if !prior.IsNull() && !prior.IsUnknown() && strings.EqualFold(prior.ValueString(), defaultMode) {
		return prior
	}
	return preferPriorFold(prior, remote)
}

// preferPriorFold returns remote unless prior differs from it only by case.
// An empty remote value keeps prior.
func preferPriorFold(prior types.String, remote string) types.String {
	if remote == "" {
		return prior
	}
	if !prior.IsNull() && !prior.IsUnknown() && strings.EqualFold(prior.ValueString(), remote) {
		return prior
	}
	return types.StringValue(remote)
}

// flattenOptionalString maps an API string where "" means unset. An explicit
// empty string in configuration is preserved.
func flattenOptionalString(prior types.String, remote string) types.String {
	if remote != "" {
		return types.StringValue(remote)
	}
	if !prior.IsNull() && !prior.IsUnknown() && prior.ValueString() == "" {
		return prior
	}
	return types.StringNull()
}

func flattenTags(ctx context.Context, prior types.List, remote []string) (types.List, diag.Diagnostics) {
	if len(remote) == 0 {
		if !prior.IsNull() && !prior.IsUnknown() && len(prior.Elements()) == 0 {
			return prior, nil
		}
		return types.ListNull(types.StringType), nil
	}

	if !prior.IsNull() && !prior.IsUnknown() {
		var priorTags []string
		if diags := prior.ElementsAs(ctx, &priorTags, false); !diags.HasError() && sameStringMultiset(priorTags, remote) {
			return prior, nil
		}
	}

	return types.ListValueFrom(ctx, types.StringType, remote)
}

func flattenScaleDownAfter(prior types.String, policy string, thresholdMS int64) types.String {
	if !prior.IsNull() && !prior.IsUnknown() {
		priorPolicy, priorThreshold, diags := parseScaleDownAfter(prior)
		if !diags.HasError() && strings.EqualFold(priorPolicy, policy) &&
			(strings.EqualFold(policy, "NONE") || priorThreshold == thresholdMS) {
			return prior
		}
	}
	return types.StringValue(formatScaleDownAfter(policy, thresholdMS))
}

func flattenDurationMS(prior types.String, remoteMS *int64) types.String {
	if remoteMS == nil {
		return prior
	}
	if !prior.IsNull() && !prior.IsUnknown() {
		if duration, err := time.ParseDuration(prior.ValueString()); err == nil && duration.Milliseconds() == *remoteMS {
			return prior
		}
	}
	return types.StringValue(formatDuration(time.Duration(*remoteMS) * time.Millisecond))
}

// flattenGPUs maps the device configs Parasail marks as selected. When they
// match the prior blocks (ignoring order and case), the prior blocks are kept.
func flattenGPUs(prior []gpuModel, configs []parasail.DedicatedDeviceConfig) []gpuModel {
	selected := make([]parasail.DedicatedDeviceConfig, 0, len(configs))
	for _, config := range configs {
		if config.Selected {
			selected = append(selected, config)
		}
	}
	if len(selected) == 0 {
		selected = configs
	}

	if gpuBlocksMatch(prior, selected) {
		return prior
	}

	out := make([]gpuModel, 0, len(selected))
	for _, config := range selected {
		out = append(out, gpuModel{
			Type:  types.StringValue(config.Device),
			Count: types.Int64Value(config.Count),
		})
	}
	return out
}

func gpuBlocksMatch(prior []gpuModel, remote []parasail.DedicatedDeviceConfig) bool {
	if len(prior) != len(remote) {
		return false
	}

	used := make([]bool, len(remote))
	for _, gpu := range prior {
		if gpu.Type.IsNull() || gpu.Type.IsUnknown() || gpu.Count.IsNull() || gpu.Count.IsUnknown() {
			return false
		}
		found := false
		for i, config := range remote {
			if !used[i] && strings.EqualFold(gpu.Type.ValueString(), config.Device) && gpu.Count.ValueInt64() == config.Count {
				used[i] = true
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func flattenAutoscaling(prior *autoscalingModel, deployment *parasail.DedicatedDeployment) *autoscalingModel {
	if !*deployment.Autoscaling {
		return nil
	}

	out := &autoscalingModel{
		MinReplicas:                 types.Int64Unknown(),
		MaxReplicas:                 types.Int64Unknown(),
		TargetConnectionsPerReplica: types.Int64Unknown(),
		SmoothingFactor:             types.Float64Unknown(),
	}
	if prior != nil {
		*out = *prior
	}

	out.MinReplicas = int64PreferRemote(out.MinReplicas, deployment.MinReplicas)
	out.MaxReplicas = int64PreferRemote(out.MaxReplicas, deployment.MaxReplicas)
	out.TargetConnectionsPerReplica = int64PreferRemote(out.TargetConnectionsPerReplica, deployment.TargetConnections)
	if deployment.AutoscalingFactor != nil {
		out.SmoothingFactor = types.Float64Value(*deployment.AutoscalingFactor)
	}
	return out
}

func int64PreferRemote(prior types.Int64, remote *int64) types.Int64 {
	if remote == nil {
		return prior
	}
	return types.Int64Value(*remote)
}

func nullIfUnknownString(value types.String) types.String {
	if value.IsUnknown() {
		return types.StringNull()
	}
	return value
}

func nullIfUnknownInt64(value types.Int64) types.Int64 {
	if value.IsUnknown() {
		return types.Int64Null()
	}
	return value
}

func sameStringMultiset(a []string, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[string]int, len(a))
	for _, v := range a {
		counts[v]++
	}
	for _, v := range b {
		counts[v]--
		if counts[v] < 0 {
			return false
		}
	}
	return true
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
			return nil, fmt.Errorf("waiting for status %s: %w", target, ctx.Err())
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
			return fmt.Errorf("waiting for deletion: %w", ctx.Err())
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

	duration, err := time.ParseDuration(strings.TrimSpace(raw))
	if err != nil || duration < 0 {
		diags.AddAttributeError(
			path.Root("scale_down_after"),
			"Invalid scale_down_after",
			scaleDownAfterHelp,
		)
		return "", 0, diags
	}
	return "TIMER", duration.Milliseconds(), diags
}

func formatScaleDownAfter(policy string, thresholdMS int64) string {
	if strings.EqualFold(policy, "NONE") {
		return "never"
	}
	return formatDuration(time.Duration(thresholdMS) * time.Millisecond)
}

// formatDuration renders a duration in the single largest whole unit
// (h, m, s, ms), falling back to Go's String form.
func formatDuration(duration time.Duration) string {
	switch {
	case duration == 0:
		return "0s"
	case duration%time.Hour == 0:
		return fmt.Sprintf("%dh", int64(duration/time.Hour))
	case duration%time.Minute == 0:
		return fmt.Sprintf("%dm", int64(duration/time.Minute))
	case duration%time.Second == 0:
		return fmt.Sprintf("%ds", int64(duration/time.Second))
	case duration%time.Millisecond == 0:
		return fmt.Sprintf("%dms", int64(duration/time.Millisecond))
	}
	return duration.String()
}

func parseOptionalDurationMS(value types.String, attribute string) (*int64, diag.Diagnostics) {
	var diags diag.Diagnostics
	if value.IsNull() || value.IsUnknown() || value.ValueString() == "" {
		return nil, diags
	}

	duration, err := time.ParseDuration(value.ValueString())
	if err != nil || duration <= 0 {
		diags.AddAttributeError(
			path.Root(attribute),
			"Invalid "+attribute,
			attribute+" must be a positive Go duration such as \"5m\", \"30s\", or \"1500ms\".",
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
