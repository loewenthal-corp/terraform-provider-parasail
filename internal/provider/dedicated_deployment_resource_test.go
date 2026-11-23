package provider

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	dschema "github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	rschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/defaults"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	parasail "github.com/loewenthal-corp/terraform-provider-parasail/internal/client"
)

func deploymentSchema(t *testing.T) rschema.Schema {
	t.Helper()

	var resp resource.SchemaResponse
	NewDedicatedDeploymentResource().Schema(context.Background(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}
	if diags := resp.Schema.ValidateImplementation(context.Background()); diags.HasError() {
		t.Fatalf("schema implementation: %v", diags)
	}
	return resp.Schema
}

func TestDedicatedDeploymentSchemaDocumentsEveryAttribute(t *testing.T) {
	schema := deploymentSchema(t)

	for name, attribute := range schema.Attributes {
		if strings.TrimSpace(attribute.GetMarkdownDescription()) == "" {
			t.Errorf("attribute %q has no description", name)
		}
	}
	for name, block := range schema.Blocks {
		if strings.TrimSpace(block.GetMarkdownDescription()) == "" {
			t.Errorf("block %q has no description", name)
		}
	}

	gpu := mustAs[rschema.ListNestedBlock](t, schema.Blocks["gpu"])
	for name, attribute := range gpu.NestedObject.Attributes {
		if attribute.GetMarkdownDescription() == "" {
			t.Errorf("gpu.%s has no description", name)
		}
	}
	autoscaling := mustAs[rschema.SingleNestedBlock](t, schema.Blocks["autoscaling"])
	for name, attribute := range autoscaling.Attributes {
		if attribute.GetMarkdownDescription() == "" {
			t.Errorf("autoscaling.%s has no description", name)
		}
	}
}

func TestDedicatedDeploymentSchemaShape(t *testing.T) {
	schema := deploymentSchema(t)

	required := []string{"name", "model"}
	for _, name := range required {
		if !schema.Attributes[name].IsRequired() {
			t.Errorf("%s should be required", name)
		}
	}

	sensitive := []string{"model_access_key", "draft_model_access_key"}
	for _, name := range sensitive {
		if !schema.Attributes[name].IsSensitive() {
			t.Errorf("%s should be sensitive", name)
		}
	}

	wantDefaults := map[string]string{
		"engine":           defaultEngine,
		"mode":             defaultMode,
		"scale_down_after": defaultScaleDownAfter,
	}
	for name, want := range wantDefaults {
		attribute := mustAs[rschema.StringAttribute](t, schema.Attributes[name])
		if !attribute.Optional || !attribute.Computed || attribute.Default == nil {
			t.Errorf("%s should be optional+computed with a default", name)
			continue
		}
		var resp defaults.StringResponse
		attribute.Default.DefaultString(context.Background(), defaults.StringRequest{}, &resp)
		if got := resp.PlanValue.ValueString(); got != want {
			t.Errorf("%s default = %q, want %q", name, got, want)
		}
	}

	computedOnly := []string{"id", "model_alias", "base_url", "status", "status_message", "deployment_available_at"}
	for _, name := range computedOnly {
		attribute := schema.Attributes[name]
		if !attribute.IsComputed() || attribute.IsOptional() || attribute.IsRequired() {
			t.Errorf("%s should be computed only", name)
		}
	}
}

func TestDedicatedDeploymentNameValidators(t *testing.T) {
	schema := deploymentSchema(t)
	validators := mustAs[rschema.StringAttribute](t, schema.Attributes["name"]).Validators

	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "simple", value: "qwen-prod"},
		{name: "minimum length", value: "ab"},
		{name: "maximum length", value: strings.Repeat("a", 32)},
		{name: "digits", value: "model-2"},
		{name: "too short", value: "a", wantErr: true},
		{name: "too long", value: strings.Repeat("a", 33), wantErr: true},
		{name: "uppercase", value: "Qwen", wantErr: true},
		{name: "underscore", value: "qwen_prod", wantErr: true},
		{name: "dot", value: "qwen.prod", wantErr: true},
		{name: "space", value: "qwen prod", wantErr: true},
		{name: "empty", value: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runStringValidators(validators, types.StringValue(tt.value)); got != tt.wantErr {
				t.Fatalf("validation error = %t, want %t", got, tt.wantErr)
			}
		})
	}
}

func TestDurationValidators(t *testing.T) {
	tests := []struct {
		name      string
		validator validator.String
		value     types.String
		wantErr   bool
	}{
		{name: "scale hours", validator: scaleDownAfterValidator{}, value: types.StringValue("8h")},
		{name: "scale minutes", validator: scaleDownAfterValidator{}, value: types.StringValue("30m")},
		{name: "scale compound", validator: scaleDownAfterValidator{}, value: types.StringValue("1h30m")},
		{name: "scale never", validator: scaleDownAfterValidator{}, value: types.StringValue("never")},
		{name: "scale NEVER", validator: scaleDownAfterValidator{}, value: types.StringValue("NEVER")},
		{name: "scale none", validator: scaleDownAfterValidator{}, value: types.StringValue("none")},
		{name: "scale zero", validator: scaleDownAfterValidator{}, value: types.StringValue("0")},
		{name: "scale null", validator: scaleDownAfterValidator{}, value: types.StringNull()},
		{name: "scale unknown", validator: scaleDownAfterValidator{}, value: types.StringUnknown()},
		{name: "scale days", validator: scaleDownAfterValidator{}, value: types.StringValue("1d"), wantErr: true},
		{name: "scale bare number", validator: scaleDownAfterValidator{}, value: types.StringValue("30"), wantErr: true},
		{name: "scale negative", validator: scaleDownAfterValidator{}, value: types.StringValue("-5m"), wantErr: true},
		{name: "scale empty", validator: scaleDownAfterValidator{}, value: types.StringValue(""), wantErr: true},
		{name: "positive seconds", validator: positiveDurationValidator{}, value: types.StringValue("30s")},
		{name: "positive millis", validator: positiveDurationValidator{}, value: types.StringValue("1500ms")},
		{name: "positive null", validator: positiveDurationValidator{}, value: types.StringNull()},
		{name: "positive zero", validator: positiveDurationValidator{}, value: types.StringValue("0s"), wantErr: true},
		{name: "positive negative", validator: positiveDurationValidator{}, value: types.StringValue("-1s"), wantErr: true},
		{name: "positive garbage", validator: positiveDurationValidator{}, value: types.StringValue("soon"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runStringValidators([]validator.String{tt.validator}, tt.value); got != tt.wantErr {
				t.Fatalf("validation error = %t, want %t", got, tt.wantErr)
			}
			if tt.validator.Description(context.Background()) == "" {
				t.Fatal("validator has no description")
			}
		})
	}
}

func TestNumericAndBlockValidators(t *testing.T) {
	schema := deploymentSchema(t)

	gpu := mustAs[rschema.ListNestedBlock](t, schema.Blocks["gpu"])
	if len(gpu.Validators) == 0 {
		t.Fatal("gpu block should require at least one entry")
	}
	count := mustAs[rschema.Int64Attribute](t, gpu.NestedObject.Attributes["count"])
	if !runInt64Validators(count.Validators, 0) || runInt64Validators(count.Validators, 1) {
		t.Fatal("gpu.count should require at least 1")
	}

	for _, name := range []string{"max_connections_per_replica", "context_length", "max_output_tokens"} {
		attribute := mustAs[rschema.Int64Attribute](t, schema.Attributes[name])
		if !runInt64Validators(attribute.Validators, 0) || runInt64Validators(attribute.Validators, 1) {
			t.Errorf("%s should require at least 1", name)
		}
	}

	autoscaling := mustAs[rschema.SingleNestedBlock](t, schema.Blocks["autoscaling"])
	minReplicas := mustAs[rschema.Int64Attribute](t, autoscaling.Attributes["min_replicas"])
	if runInt64Validators(minReplicas.Validators, 0) || !runInt64Validators(minReplicas.Validators, -1) {
		t.Error("autoscaling.min_replicas should allow 0 and reject negatives")
	}
	maxReplicas := mustAs[rschema.Int64Attribute](t, autoscaling.Attributes["max_replicas"])
	if !runInt64Validators(maxReplicas.Validators, 0) {
		t.Error("autoscaling.max_replicas should reject 0")
	}
}

func TestExpandDeploymentFixedSize(t *testing.T) {
	ctx := context.Background()
	model := baseModel(t)
	model.Tags = mustStringList(t, "a", "b")
	model.Description = types.StringValue("desc")
	model.ModelAccessKey = types.StringValue("hf_secret")
	model.MaxRequestDuration = types.StringValue("5m")
	model.MaxConnectionsPerReplica = types.Int64Value(8)
	model.ContextLength = types.Int64Value(4096)
	model.MaxOutputTokens = types.Int64Value(512)
	model.ChatTemplate = types.StringValue("{{ messages }}")
	model.GPUs = []gpuModel{
		{Type: types.StringValue("RTX5090"), Count: types.Int64Value(1)},
		{Type: types.StringValue("H100SXM"), Count: types.Int64Value(2)},
	}

	got, diags := expandDeployment(ctx, model)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}

	if got.DeploymentName != "qwen-test" || got.ModelName != "Qwen/Qwen2.5-7B-Instruct" {
		t.Fatalf("name/model = %q/%q", got.DeploymentName, got.ModelName)
	}
	if got.Engine != defaultEngine || got.EngineTask != defaultMode {
		t.Fatalf("engine/mode = %q/%q", got.Engine, got.EngineTask)
	}
	if got.ScaleDownPolicy != "TIMER" || got.ScaleDownThreshold != 8*3600*1000 {
		t.Fatalf("scale down = %s/%d", got.ScaleDownPolicy, got.ScaleDownThreshold)
	}
	if got.Autoscaling == nil || *got.Autoscaling || got.Replicas != 1 || got.MinReplicas != nil {
		t.Fatalf("fixed size autoscaling fields = %v/%d/%v", got.Autoscaling, got.Replicas, got.MinReplicas)
	}
	if !reflect.DeepEqual(got.Tags, []string{"a", "b"}) || got.Description != "desc" || got.ModelAccessKey != "hf_secret" {
		t.Fatalf("tags/description/key = %v/%q/%q", got.Tags, got.Description, got.ModelAccessKey)
	}
	if got.MaxRequestDuration == nil || *got.MaxRequestDuration != 300000 {
		t.Fatalf("max request duration = %v", got.MaxRequestDuration)
	}
	if *got.MaxConnectionsPerReplica != 8 || *got.ContextLengthOverride != 4096 || *got.MaxCompletionTokens != 512 {
		t.Fatal("numeric overrides not mapped")
	}
	if got.ChatTemplateOverride != "{{ messages }}" {
		t.Fatalf("chat template = %q", got.ChatTemplateOverride)
	}
	wantDevices := []parasail.DedicatedDeviceConfig{
		{Device: "RTX5090", Count: 1, Selected: true},
		{Device: "H100SXM", Count: 2, Selected: true},
	}
	if !reflect.DeepEqual(got.DeviceConfigs, wantDevices) {
		t.Fatalf("device configs = %#v", got.DeviceConfigs)
	}
}

func TestExpandDeploymentAutoscaling(t *testing.T) {
	model := baseModel(t)
	model.Autoscaling = &autoscalingModel{
		MinReplicas:                 types.Int64Unknown(),
		MaxReplicas:                 types.Int64Value(4),
		TargetConnectionsPerReplica: types.Int64Value(16),
		SmoothingFactor:             types.Float64Value(0.3),
	}

	got, diags := expandDeployment(context.Background(), model)
	if diags.HasError() {
		t.Fatalf("diags: %v", diags)
	}
	if got.Autoscaling == nil || !*got.Autoscaling {
		t.Fatal("autoscaling should be enabled")
	}
	if got.Replicas != 1 || *got.MinReplicas != 1 {
		t.Fatalf("min replicas should default to 1, got replicas=%d min=%d", got.Replicas, *got.MinReplicas)
	}
	if *got.MaxReplicas != 4 || *got.TargetConnections != 16 || *got.AutoscalingFactor != 0.3 {
		t.Fatal("autoscaling settings not mapped")
	}

	model.Autoscaling.MinReplicas = types.Int64Value(2)
	model.Autoscaling.SmoothingFactor = types.Float64Null()
	got, _ = expandDeployment(context.Background(), model)
	if got.Replicas != 2 || *got.MinReplicas != 2 || got.AutoscalingFactor != nil {
		t.Fatalf("replicas=%d min=%d factor=%v", got.Replicas, *got.MinReplicas, got.AutoscalingFactor)
	}
}

func TestExpandDeploymentErrors(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*dedicatedDeploymentResourceModel)
		path   path.Path
	}{
		{name: "no gpu", mutate: func(m *dedicatedDeploymentResourceModel) { m.GPUs = nil }, path: path.Root("gpu")},
		{name: "bad scale down", mutate: func(m *dedicatedDeploymentResourceModel) { m.ScaleDownAfter = types.StringValue("1d") }, path: path.Root("scale_down_after")},
		{name: "bad request duration", mutate: func(m *dedicatedDeploymentResourceModel) { m.MaxRequestDuration = types.StringValue("0s") }, path: path.Root("max_request_duration")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := baseModel(t)
			tt.mutate(&model)
			_, diags := expandDeployment(context.Background(), model)
			if !diags.HasError() {
				t.Fatal("expected error")
			}
			found := false
			for _, d := range diags.Errors() {
				if withPath, ok := d.(interface{ Path() path.Path }); ok && withPath.Path().Equal(tt.path) {
					found = true
				}
			}
			if !found {
				t.Fatalf("no error at %s: %v", tt.path, diags)
			}
		})
	}
}

func TestScaleDownAfterRoundTrip(t *testing.T) {
	tests := []struct {
		in          string
		policy      string
		thresholdMS int64
		formatted   string
	}{
		{in: "8h", policy: "TIMER", thresholdMS: 28800000, formatted: "8h"},
		{in: "480m", policy: "TIMER", thresholdMS: 28800000, formatted: "8h"},
		{in: "30m", policy: "TIMER", thresholdMS: 1800000, formatted: "30m"},
		{in: "90s", policy: "TIMER", thresholdMS: 90000, formatted: "90s"},
		{in: "1500ms", policy: "TIMER", thresholdMS: 1500, formatted: "1500ms"},
		{in: "never", policy: "NONE", thresholdMS: 0, formatted: "never"},
		{in: "0s", policy: "NONE", thresholdMS: 0, formatted: "never"},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			policy, threshold, diags := parseScaleDownAfter(types.StringValue(tt.in))
			if diags.HasError() {
				t.Fatal(diags)
			}
			if policy != tt.policy || threshold != tt.thresholdMS {
				t.Fatalf("parse = %s/%d, want %s/%d", policy, threshold, tt.policy, tt.thresholdMS)
			}
			if got := formatScaleDownAfter(policy, threshold); got != tt.formatted {
				t.Fatalf("format = %q, want %q", got, tt.formatted)
			}
			// The user's spelling survives a read when it means the same thing.
			if got := flattenScaleDownAfter(types.StringValue(tt.in), policy, threshold); got.ValueString() != tt.in {
				t.Fatalf("flatten kept %q, want %q", got.ValueString(), tt.in)
			}
		})
	}

	// Parasail reports a threshold alongside NONE; it must not cause drift.
	if got := flattenScaleDownAfter(types.StringValue("never"), "NONE", 28800000); got.ValueString() != "never" {
		t.Fatalf("NONE with threshold = %q", got.ValueString())
	}
	// Real changes are reported.
	if got := flattenScaleDownAfter(types.StringValue("8h"), "TIMER", 1800000); got.ValueString() != "30m" {
		t.Fatalf("drift = %q, want 30m", got.ValueString())
	}
	if got := flattenScaleDownAfter(types.StringNull(), "NONE", 0); got.ValueString() != "never" {
		t.Fatalf("import = %q, want never", got.ValueString())
	}
}

func TestFlattenDeploymentImport(t *testing.T) {
	ctx := context.Background()
	autoscaling := true
	minReplicas, maxReplicas, target := int64(1), int64(3), int64(16)
	factor := 0.3
	maxDuration := int64(300000)
	maxConnections := int64(32)
	available := int64(1767225600000)

	remote := &parasail.DedicatedDeployment{
		ID:                       12345,
		DeploymentName:           "qwen-test",
		ExternalAlias:            "acct-qwen-test",
		Description:              "imported",
		Tags:                     []string{"a"},
		ModelName:                "Qwen/Qwen2.5-7B-Instruct",
		BaseURL:                  "https://api.parasail.io/v1",
		ScaleDownPolicy:          "TIMER",
		ScaleDownThreshold:       1800000,
		EngineTask:               "GENERATE",
		Engine:                   "VLLM",
		ChatTemplateOverride:     "",
		Autoscaling:              &autoscaling,
		AutoscalingFactor:        &factor,
		MinReplicas:              &minReplicas,
		MaxReplicas:              &maxReplicas,
		TargetConnections:        &target,
		MaxRequestDuration:       &maxDuration,
		MaxConnectionsPerReplica: &maxConnections,
		DeploymentAvailableAt:    &available,
		Status:                   &parasail.DeploymentStatus{Status: "ONLINE"},
		DeviceConfigs: []parasail.DedicatedDeviceConfig{
			{Device: "B200SXM", Count: 4, Selected: true},
			{Device: "H100SXM", Count: 1, Selected: false},
			{Device: "B300SXM", Count: 4, Selected: true},
		},
	}

	// Import starts from a state that only has the ID.
	model := emptyModel()
	model.ID = types.StringValue("12345")
	if diags := flattenDeployment(ctx, &model, remote); diags.HasError() {
		t.Fatal(diags)
	}

	assertString(t, "id", model.ID, "12345")
	assertString(t, "name", model.Name, "qwen-test")
	assertString(t, "model", model.Model, "Qwen/Qwen2.5-7B-Instruct")
	assertString(t, "engine", model.Engine, "VLLM")
	assertString(t, "mode", model.Mode, "GENERATE")
	assertString(t, "description", model.Description, "imported")
	assertString(t, "scale_down_after", model.ScaleDownAfter, "30m")
	assertString(t, "max_request_duration", model.MaxRequestDuration, "5m")
	assertString(t, "model_alias", model.ModelAlias, "acct-qwen-test")
	assertString(t, "base_url", model.BaseURL, "https://api.parasail.io/v1")
	assertString(t, "status", model.Status, "ONLINE")
	if !model.ChatTemplate.IsNull() || !model.DraftModel.IsNull() || !model.StatusMessage.IsNull() {
		t.Fatal("empty strings should map to null")
	}
	if !model.ModelAccessKey.IsNull() || !model.WaitForOnline.IsNull() {
		t.Fatal("write-only and provider-side settings must stay null on import")
	}
	if model.MaxConnectionsPerReplica.ValueInt64() != 32 || !model.ContextLength.IsNull() || !model.MaxOutputTokens.IsNull() {
		t.Fatal("numeric settings not mapped")
	}
	if model.DeploymentAvailableAt.ValueInt64() != available {
		t.Fatal("deployment_available_at not mapped")
	}
	if got := model.Tags.Elements(); len(got) != 1 || mustAs[types.String](t, got[0]).ValueString() != "a" {
		t.Fatalf("tags = %v", got)
	}
	wantGPUs := []gpuModel{
		{Type: types.StringValue("B200SXM"), Count: types.Int64Value(4)},
		{Type: types.StringValue("B300SXM"), Count: types.Int64Value(4)},
	}
	if !reflect.DeepEqual(model.GPUs, wantGPUs) {
		t.Fatalf("gpus = %v, want only the selected configs", model.GPUs)
	}
	want := &autoscalingModel{
		MinReplicas:                 types.Int64Value(1),
		MaxReplicas:                 types.Int64Value(3),
		TargetConnectionsPerReplica: types.Int64Value(16),
		SmoothingFactor:             types.Float64Value(0.3),
	}
	if !reflect.DeepEqual(model.Autoscaling, want) {
		t.Fatalf("autoscaling = %#v", model.Autoscaling)
	}
}

func TestFlattenDeploymentPreservesConfigSpelling(t *testing.T) {
	ctx := context.Background()
	fixed := false
	remote := &parasail.DedicatedDeployment{
		ID:                 1,
		DeploymentName:     "qwen-test",
		ModelName:          "qwen/qwen2.5-7b-instruct",
		Engine:             "VLLM",
		EngineTask:         "GENERATE",
		ScaleDownPolicy:    "TIMER",
		ScaleDownThreshold: 28800000,
		Tags:               []string{"b", "a"},
		Autoscaling:        &fixed,
		DeviceConfigs: []parasail.DedicatedDeviceConfig{
			{Device: "RTX6000PRO", Count: 1, Selected: true},
			{Device: "RTX5090", Count: 1, Selected: true},
		},
	}

	model := baseModel(t)
	model.Engine = types.StringValue("vllm")
	model.ScaleDownAfter = types.StringValue("480m")
	model.Tags = mustStringList(t, "a", "b")
	model.GPUs = []gpuModel{
		{Type: types.StringValue("rtx5090"), Count: types.Int64Value(1)},
		{Type: types.StringValue("RTX6000PRO"), Count: types.Int64Value(1)},
	}
	before := model

	if diags := flattenDeployment(ctx, &model, remote); diags.HasError() {
		t.Fatal(diags)
	}

	assertString(t, "model", model.Model, before.Model.ValueString())
	assertString(t, "engine", model.Engine, "vllm")
	assertString(t, "mode", model.Mode, "AUTO")
	assertString(t, "scale_down_after", model.ScaleDownAfter, "480m")
	if !model.Tags.Equal(before.Tags) {
		t.Fatalf("tags reordered: %v", model.Tags)
	}
	if !reflect.DeepEqual(model.GPUs, before.GPUs) {
		t.Fatalf("gpu blocks reordered: %v", model.GPUs)
	}
	if model.Autoscaling != nil {
		t.Fatal("fixed-size deployment should have no autoscaling block")
	}
}

func TestFlattenDeploymentReportsDrift(t *testing.T) {
	ctx := context.Background()
	remote := &parasail.DedicatedDeployment{
		ID:             1,
		DeploymentName: "qwen-test",
		ModelName:      "Qwen/Qwen2.5-7B-Instruct",
		Engine:         "SGLANG",
		EngineTask:     "EMBED",
		DeviceConfigs: []parasail.DedicatedDeviceConfig{
			{Device: "H100SXM", Count: 2, Selected: true},
		},
	}

	model := baseModel(t)
	model.Mode = types.StringValue("GENERATE")
	model.Description = types.StringValue("was set")

	if diags := flattenDeployment(ctx, &model, remote); diags.HasError() {
		t.Fatal(diags)
	}
	assertString(t, "engine", model.Engine, "SGLANG")
	assertString(t, "mode", model.Mode, "EMBED")
	if !model.Description.IsNull() {
		t.Fatal("cleared description should be reported as drift")
	}
	if len(model.GPUs) != 1 || model.GPUs[0].Type.ValueString() != "H100SXM" || model.GPUs[0].Count.ValueInt64() != 2 {
		t.Fatalf("gpus = %v", model.GPUs)
	}
}

func TestFlattenDeploymentResolvesUnknowns(t *testing.T) {
	ctx := context.Background()
	autoscaling := true
	maxReplicas := int64(2)
	remote := &parasail.DedicatedDeployment{
		ID:             9,
		DeploymentName: "qwen-test",
		ModelName:      "Qwen/Qwen2.5-7B-Instruct",
		Autoscaling:    &autoscaling,
		MaxReplicas:    &maxReplicas,
	}

	// A planned create: computed values are unknown until the API responds.
	model := baseModel(t)
	model.ID = types.StringUnknown()
	model.ModelAlias = types.StringUnknown()
	model.BaseURL = types.StringUnknown()
	model.Status = types.StringUnknown()
	model.StatusMessage = types.StringUnknown()
	model.DeploymentAvailableAt = types.Int64Unknown()
	model.MaxConnectionsPerReplica = types.Int64Unknown()
	model.ContextLength = types.Int64Unknown()
	model.MaxOutputTokens = types.Int64Unknown()
	model.MaxRequestDuration = types.StringUnknown()
	model.Autoscaling = &autoscalingModel{
		MinReplicas:                 types.Int64Unknown(),
		MaxReplicas:                 types.Int64Unknown(),
		TargetConnectionsPerReplica: types.Int64Value(16),
		SmoothingFactor:             types.Float64Unknown(),
	}

	if diags := flattenDeployment(ctx, &model, remote); diags.HasError() {
		t.Fatal(diags)
	}

	assertString(t, "id", model.ID, "9")
	for name, value := range map[string]attr.Value{
		"model_alias":                    model.ModelAlias,
		"base_url":                       model.BaseURL,
		"status":                         model.Status,
		"status_message":                 model.StatusMessage,
		"deployment_available_at":        model.DeploymentAvailableAt,
		"max_connections_per_replica":    model.MaxConnectionsPerReplica,
		"context_length":                 model.ContextLength,
		"max_output_tokens":              model.MaxOutputTokens,
		"max_request_duration":           model.MaxRequestDuration,
		"autoscaling.min_replicas":       model.Autoscaling.MinReplicas,
		"autoscaling.smoothing_factor":   model.Autoscaling.SmoothingFactor,
		"autoscaling.max_replicas":       model.Autoscaling.MaxReplicas,
		"autoscaling.target_connections": model.Autoscaling.TargetConnectionsPerReplica,
	} {
		if value.IsUnknown() {
			t.Errorf("%s is still unknown after apply", name)
		}
	}
	if model.Autoscaling.MaxReplicas.ValueInt64() != 2 {
		t.Fatal("max_replicas should come from the API")
	}
	// Not echoed by the API: keep the planned value.
	if model.Autoscaling.TargetConnectionsPerReplica.ValueInt64() != 16 {
		t.Fatal("target_connections_per_replica should keep the planned value")
	}
}

func TestFlattenOptionalStringAndTags(t *testing.T) {
	ctx := context.Background()

	if got := flattenOptionalString(types.StringValue(""), ""); got.IsNull() || got.ValueString() != "" {
		t.Fatal("explicit empty string should be preserved")
	}
	if got := flattenOptionalString(types.StringNull(), ""); !got.IsNull() {
		t.Fatal("unset should stay null")
	}

	empty := mustStringList(t)
	if got, _ := flattenTags(ctx, empty, nil); !got.Equal(empty) {
		t.Fatal("explicit empty tags should be preserved")
	}
	if got, _ := flattenTags(ctx, types.ListNull(types.StringType), []string{}); !got.IsNull() {
		t.Fatal("no tags should be null")
	}
	got, _ := flattenTags(ctx, mustStringList(t, "a"), []string{"a", "b"})
	if len(got.Elements()) != 2 {
		t.Fatal("added remote tag should be reported")
	}
}

func TestFlattenGPUsWithoutSelectedFlags(t *testing.T) {
	got := flattenGPUs(nil, []parasail.DedicatedDeviceConfig{{Device: "RTX5090", Count: 1}})
	if len(got) != 1 || got[0].Type.ValueString() != "RTX5090" {
		t.Fatalf("gpus = %v", got)
	}
}

func TestFormatDuration(t *testing.T) {
	ms := func(v int64) *int64 { return &v }
	tests := []struct {
		prior types.String
		ms    *int64
		want  types.String
	}{
		{prior: types.StringNull(), ms: nil, want: types.StringNull()},
		{prior: types.StringValue("300s"), ms: ms(300000), want: types.StringValue("300s")},
		{prior: types.StringValue("300s"), ms: ms(60000), want: types.StringValue("1m")},
		{prior: types.StringNull(), ms: ms(1500), want: types.StringValue("1500ms")},
		{prior: types.StringValue("5m"), ms: nil, want: types.StringValue("5m")},
	}
	for _, tt := range tests {
		if got := flattenDurationMS(tt.prior, tt.ms); !got.Equal(tt.want) {
			t.Errorf("flattenDurationMS(%v, %v) = %v, want %v", tt.prior, tt.ms, got, tt.want)
		}
	}
}

func TestParseDeploymentID(t *testing.T) {
	if id, err := parseDeploymentID(types.StringValue("12345")); err != nil || id != 12345 {
		t.Fatalf("id = %d, err = %v", id, err)
	}
	for _, value := range []types.String{types.StringNull(), types.StringUnknown(), types.StringValue("abc")} {
		if _, err := parseDeploymentID(value); err == nil {
			t.Errorf("parseDeploymentID(%v) succeeded", value)
		}
	}
}

func TestShouldWaitForOnline(t *testing.T) {
	if !shouldWaitForOnline(types.BoolNull()) || !shouldWaitForOnline(types.BoolValue(true)) {
		t.Fatal("wait_for_online should default to true")
	}
	if shouldWaitForOnline(types.BoolValue(false)) {
		t.Fatal("wait_for_online = false should not wait")
	}
}

func baseModel(t *testing.T) dedicatedDeploymentResourceModel {
	t.Helper()
	model := emptyModel()
	model.Name = types.StringValue("qwen-test")
	model.Model = types.StringValue("Qwen/Qwen2.5-7B-Instruct")
	model.Engine = types.StringValue(defaultEngine)
	model.Mode = types.StringValue(defaultMode)
	model.ScaleDownAfter = types.StringValue(defaultScaleDownAfter)
	model.GPUs = []gpuModel{{Type: types.StringValue("RTX5090"), Count: types.Int64Value(1)}}
	return model
}

func emptyModel() dedicatedDeploymentResourceModel {
	return dedicatedDeploymentResourceModel{
		ID:                       types.StringNull(),
		Name:                     types.StringNull(),
		Model:                    types.StringNull(),
		ModelAccessKey:           types.StringNull(),
		Description:              types.StringNull(),
		Tags:                     types.ListNull(types.StringType),
		Engine:                   types.StringNull(),
		Mode:                     types.StringNull(),
		ScaleDownAfter:           types.StringNull(),
		DraftModel:               types.StringNull(),
		DraftModelAccessKey:      types.StringNull(),
		MaxConnectionsPerReplica: types.Int64Null(),
		ContextLength:            types.Int64Null(),
		MaxOutputTokens:          types.Int64Null(),
		MaxRequestDuration:       types.StringNull(),
		ChatTemplate:             types.StringNull(),
		WaitForOnline:            types.BoolNull(),
		ModelAlias:               types.StringNull(),
		BaseURL:                  types.StringNull(),
		Status:                   types.StringNull(),
		StatusMessage:            types.StringNull(),
		DeploymentAvailableAt:    types.Int64Null(),
	}
}

func mustStringList(t *testing.T, values ...string) types.List {
	t.Helper()
	if values == nil {
		values = []string{}
	}
	list, diags := types.ListValueFrom(context.Background(), types.StringType, values)
	if diags.HasError() {
		t.Fatal(diags)
	}
	return list
}

func assertString(t *testing.T, name string, got types.String, want string) {
	t.Helper()
	if got.IsNull() || got.IsUnknown() || got.ValueString() != want {
		t.Fatalf("%s = %v, want %q", name, got, want)
	}
}

func runStringValidators(validators []validator.String, value types.String) bool {
	hasError := false
	for _, v := range validators {
		resp := &validator.StringResponse{}
		v.ValidateString(context.Background(), validator.StringRequest{Path: path.Root("test"), ConfigValue: value}, resp)
		hasError = hasError || resp.Diagnostics.HasError()
	}
	return hasError
}

func runInt64Validators(validators []validator.Int64, value int64) bool {
	hasError := false
	for _, v := range validators {
		resp := &validator.Int64Response{}
		v.ValidateInt64(context.Background(), validator.Int64Request{Path: path.Root("test"), ConfigValue: types.Int64Value(value)}, resp)
		hasError = hasError || resp.Diagnostics.HasError()
	}
	return hasError
}

func TestDataSourceAndProviderSchemasDocumentEveryAttribute(t *testing.T) {
	ctx := context.Background()

	var providerResp provider.SchemaResponse
	New("test")().Schema(ctx, provider.SchemaRequest{}, &providerResp)
	for name, attribute := range providerResp.Schema.Attributes {
		if attribute.GetMarkdownDescription() == "" {
			t.Errorf("provider attribute %q has no description", name)
		}
	}

	for _, factory := range New("test")().DataSources(ctx) {
		ds := factory()
		var meta datasource.MetadataResponse
		ds.Metadata(ctx, datasource.MetadataRequest{ProviderTypeName: "parasail"}, &meta)
		var resp datasource.SchemaResponse
		ds.Schema(ctx, datasource.SchemaRequest{}, &resp)
		if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
			t.Fatalf("%s schema: %v", meta.TypeName, diags)
		}
		if resp.Schema.GetMarkdownDescription() == "" {
			t.Errorf("%s has no description", meta.TypeName)
		}
		checkDataSourceAttributes(t, meta.TypeName, resp.Schema.Attributes)
	}
}

func checkDataSourceAttributes(t *testing.T, prefix string, attributes map[string]dschema.Attribute) {
	t.Helper()
	for name, attribute := range attributes {
		if attribute.GetMarkdownDescription() == "" {
			t.Errorf("%s.%s has no description", prefix, name)
		}
		if nested, ok := attribute.(dschema.ListNestedAttribute); ok {
			checkDataSourceAttributes(t, prefix+"."+name, nested.NestedObject.Attributes)
		}
	}
}

func mustAs[T any](t *testing.T, value any) T {
	t.Helper()
	typed, ok := value.(T)
	if !ok {
		var zero T
		t.Fatalf("value is %T, want %T", value, zero)
	}
	return typed
}
