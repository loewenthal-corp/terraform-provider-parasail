package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// These structs model the documented Parasail Control API JSON contracts.

type DedicatedDeployment struct {
	ID                       int64                   `json:"id,omitempty"`
	AccountName              string                  `json:"accountName,omitempty"`
	DeploymentName           string                  `json:"deploymentName,omitempty"`
	DisplayName              string                  `json:"displayName,omitempty"`
	ExternalAlias            string                  `json:"externalAlias,omitempty"`
	Description              string                  `json:"description,omitempty"`
	Tags                     []string                `json:"tags,omitempty"`
	ModelName                string                  `json:"modelName,omitempty"`
	ModelAccessKey           string                  `json:"modelAccessKey,omitempty"`
	BaseURL                  string                  `json:"baseUrl,omitempty"`
	Replicas                 int64                   `json:"replicas,omitempty"`
	ScaleDownPolicy          string                  `json:"scaleDownPolicy,omitempty"`
	ScaleDownThreshold       int64                   `json:"scaleDownThreshold,omitempty"`
	DraftModelName           string                  `json:"draftModelName,omitempty"`
	DraftModelAccessKey      string                  `json:"draftModelAccessKey,omitempty"`
	SpeculativeConfig        *SpeculativeConfig      `json:"speculativeConfig,omitempty"`
	EngineTask               string                  `json:"engineTask,omitempty"`
	Batch                    bool                    `json:"batch,omitempty"`
	MaxConnectionsPerReplica *int64                  `json:"maxConnectionsPerReplica,omitempty"`
	Status                   *DeploymentStatus       `json:"status,omitempty"`
	Engine                   string                  `json:"engine,omitempty"`
	ContextLength            *int64                  `json:"contextLength,omitempty"`
	ContextLengthOverride    *int64                  `json:"contextLengthOverride,omitempty"`
	MaxCompletionTokens      *int64                  `json:"maxCompletionTokens,omitempty"`
	MaxRequestDuration       *int64                  `json:"maxRequestDuration,omitempty"`
	ChatTemplateOverride     string                  `json:"chatTemplateOverride,omitempty"`
	Autoscaling              *bool                   `json:"autoscaling,omitempty"`
	AutoscalingFactor        *float64                `json:"autoscalingFactor,omitempty"`
	TargetConnections        *int64                  `json:"targetConnectionsPerReplica,omitempty"`
	MinReplicas              *int64                  `json:"minReplicas,omitempty"`
	MaxReplicas              *int64                  `json:"maxReplicas,omitempty"`
	CreatedAt                *int64                  `json:"createdAt,omitempty"`
	StartedAt                *int64                  `json:"startedAt,omitempty"`
	DeploymentAvailableAt    *int64                  `json:"deploymentAvailableAt,omitempty"`
	ModifiedAt               *int64                  `json:"modifiedAt,omitempty"`
	DeviceConfigs            []DedicatedDeviceConfig `json:"deviceConfigs,omitempty"`
}

type DedicatedDeviceConfig struct {
	Device                 string   `json:"device,omitempty"`
	Count                  int64    `json:"count,omitempty"`
	DisplayName            string   `json:"displayName,omitempty"`
	Cost                   *float64 `json:"cost,omitempty"`
	EstimatedSingleUserTPS *float64 `json:"estimatedSingleUserTps,omitempty"`
	EstimatedSystemTPS     *float64 `json:"estimatedSystemTps,omitempty"`
	Recommended            bool     `json:"recommended,omitempty"`
	LimitedContext         bool     `json:"limitedContext,omitempty"`
	Available              bool     `json:"available,omitempty"`
	Selected               bool     `json:"selected,omitempty"`
}

type DeploymentStatus struct {
	ID                int64                      `json:"id,omitempty"`
	Status            string                     `json:"status,omitempty"`
	StatusMessage     string                     `json:"statusMessage,omitempty"`
	StatusLastUpdated *int64                     `json:"statusLastUpdated,omitempty"`
	Instances         []DeploymentInstanceStatus `json:"instances,omitempty"`
}

type DeploymentInstanceStatus struct {
	ID                int64  `json:"id,omitempty"`
	Name              string `json:"name,omitempty"`
	Status            string `json:"status,omitempty"`
	StatusMessage     string `json:"statusMessage,omitempty"`
	StatusLastUpdated *int64 `json:"statusLastUpdated,omitempty"`
	ObservedContext   *int64 `json:"observedContext,omitempty"`
	KVCacheSize       *int64 `json:"kvCacheSize,omitempty"`
}

type SpeculativeConfig struct {
	Method               string `json:"method,omitempty"`
	NumSpeculativeTokens *int64 `json:"numSpeculativeTokens,omitempty"`
	MaxModelLen          *int64 `json:"maxModelLen,omitempty"`
	DisableByBatchSize   *int64 `json:"disableByBatchSize,omitempty"`
	PromptLookupMax      *int64 `json:"promptLookupMax,omitempty"`
	PromptLookupMin      *int64 `json:"promptLookupMin,omitempty"`
	EagleTopK            *int64 `json:"eagleTopK,omitempty"`
	NumSteps             *int64 `json:"numSteps,omitempty"`
}

type EngineSupport struct {
	Messages          []LogMessage     `json:"messages,omitempty"`
	Supported         bool             `json:"supported,omitempty"`
	Known             bool             `json:"known,omitempty"`
	Properties        *ModelProperties `json:"properties,omitempty"`
	SupportingEngines []string         `json:"supportingEngines,omitempty"`
}

type LogMessage struct {
	Content string `json:"content,omitempty"`
	Level   string `json:"level,omitempty"`
}

type ModelProperties struct {
	Context                   *int64 `json:"context,omitempty"`
	ParameterQuantizationBits *int64 `json:"parameter_quantization_bits,omitempty"`
	QuantizationHighLevel     string `json:"quantization_high_level,omitempty"`
	QuantizationDetail        string `json:"quantization_detail,omitempty"`
	ParameterCount            *int64 `json:"parameter_count,omitempty"`
	ParameterTotalBytes       *int64 `json:"parameter_total_bytes,omitempty"`
	BaseModel                 string `json:"base_model,omitempty"`
}

type DeviceConfigsRequest struct {
	Engine              string
	ModelName           string
	ModelAccessKey      string
	DraftModelName      string
	DraftModelAccessKey string
	ModelAccessKeyFrom  int64
}

type ModelSupportRequest struct {
	Engine             string
	ModelName          string
	ModelAccessKey     string
	ModelAccessKeyFrom int64
}

func (c *Client) ListDeployments(ctx context.Context) ([]DedicatedDeployment, error) {
	var out []DedicatedDeployment
	err := c.do(ctx, http.MethodGet, "/dedicated/deployments", nil, nil, &out)
	return out, err
}

func (c *Client) GetDeployment(ctx context.Context, id int64) (*DedicatedDeployment, error) {
	var out DedicatedDeployment
	err := c.do(ctx, http.MethodGet, fmt.Sprintf("/dedicated/deployments/%d", id), nil, nil, &out)
	return &out, err
}

func (c *Client) CreateDeployment(ctx context.Context, deployment DedicatedDeployment) (*DedicatedDeployment, error) {
	var out DedicatedDeployment
	err := c.do(ctx, http.MethodPost, "/dedicated/deployments", nil, deployment, &out)
	return &out, err
}

func (c *Client) UpdateDeployment(ctx context.Context, id int64, deployment DedicatedDeployment) (*DedicatedDeployment, error) {
	var out DedicatedDeployment
	err := c.do(ctx, http.MethodPut, fmt.Sprintf("/dedicated/deployments/%d", id), nil, deployment, &out)
	return &out, err
}

func (c *Client) DeleteDeployment(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodDelete, fmt.Sprintf("/dedicated/deployments/%d", id), nil, nil, nil)
}

func (c *Client) ResumeDeployment(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/dedicated/deployments/%d/resume", id), nil, nil, nil)
}

func (c *Client) PauseDeployment(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodPost, fmt.Sprintf("/dedicated/deployments/%d/pause", id), nil, nil, nil)
}

func (c *Client) DeviceConfigs(ctx context.Context, request DeviceConfigsRequest) ([]DedicatedDeviceConfig, error) {
	query := url.Values{}
	addQuery(query, "engine", request.Engine)
	addQuery(query, "modelName", request.ModelName)
	addQuery(query, "modelAccessKey", request.ModelAccessKey)
	addQuery(query, "draftModelName", request.DraftModelName)
	addQuery(query, "draftModelAccessKey", request.DraftModelAccessKey)
	if request.ModelAccessKeyFrom > 0 {
		query.Set("modelAccessKeyFrom", strconv.FormatInt(request.ModelAccessKeyFrom, 10))
	}

	var out []DedicatedDeviceConfig
	err := c.do(ctx, http.MethodGet, "/dedicated/devices", query, nil, &out)
	return out, err
}

func (c *Client) ModelSupport(ctx context.Context, request ModelSupportRequest) (*EngineSupport, error) {
	query := url.Values{}
	addQuery(query, "engine", request.Engine)
	addQuery(query, "modelName", request.ModelName)
	addQuery(query, "modelAccessKey", request.ModelAccessKey)
	if request.ModelAccessKeyFrom > 0 {
		query.Set("modelAccessKeyFrom", strconv.FormatInt(request.ModelAccessKeyFrom, 10))
	}

	var out EngineSupport
	err := c.do(ctx, http.MethodGet, "/dedicated/support", query, nil, &out)
	return &out, err
}

func addQuery(query url.Values, key string, value string) {
	if value != "" {
		query.Set(key, value)
	}
}
