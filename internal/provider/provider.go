package provider

import (
	"context"
	"os"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	parasail "github.com/loewenthal-corp/terraform-provider-parasail/internal/client"
)

type apiConfig struct {
	Client           *parasail.Client
	PollInterval     time.Duration
	OperationTimeout time.Duration
}

type parasailProvider struct {
	version string
}

type providerModel struct {
	APIKey                  types.String `tfsdk:"api_key"`
	Endpoint                types.String `tfsdk:"endpoint"`
	RequestTimeoutSeconds   types.Int64  `tfsdk:"request_timeout_seconds"`
	PollIntervalSeconds     types.Int64  `tfsdk:"poll_interval_seconds"`
	OperationTimeoutMinutes types.Int64  `tfsdk:"operation_timeout_minutes"`
}

var _ provider.Provider = &parasailProvider{}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &parasailProvider{version: version}
	}
}

func (p *parasailProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "parasail"
	resp.Version = p.version
}

func (p *parasailProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Unofficial Terraform and OpenTofu provider for [Parasail](https://www.parasail.io/) dedicated model deployments. " +
			"It is a community project, not affiliated with or endorsed by Parasail, and uses only Parasail's public Control API.",
		Attributes: map[string]schema.Attribute{
			"api_key": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "Parasail API key. Can also be set with the `PARASAIL_API_KEY` environment variable; the provider argument takes precedence.",
			},
			"endpoint": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Parasail Control API base URL. Must be an absolute URL. Can also be set with `PARASAIL_ENDPOINT`. Defaults to `https://api.parasail.io/api/v1`.",
			},
			"request_timeout_seconds": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Timeout for each HTTP request, in seconds. Defaults to `60`; values of 0 or less use the default.",
			},
			"poll_interval_seconds": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "How often to poll deployment status while waiting for `ONLINE` or for deletion, in seconds. Defaults to `30`; values of 0 or less use the default.",
			},
			"operation_timeout_minutes": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Maximum time to wait for a deployment to become `ONLINE` or to be deleted, in minutes. Defaults to `60`; values of 0 or less use the default.",
			},
		},
	}
}

func (p *parasailProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiKey := os.Getenv("PARASAIL_API_KEY")
	if !config.APIKey.IsNull() {
		apiKey = config.APIKey.ValueString()
	}
	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Missing Parasail API key",
			"Set api_key in the provider configuration or PARASAIL_API_KEY in the environment.",
		)
		return
	}

	endpoint := os.Getenv("PARASAIL_ENDPOINT")
	if !config.Endpoint.IsNull() {
		endpoint = config.Endpoint.ValueString()
	}
	if endpoint == "" {
		endpoint = parasail.DefaultEndpoint
	}

	requestTimeoutSeconds := int64Default(config.RequestTimeoutSeconds, 60)
	pollIntervalSeconds := int64Default(config.PollIntervalSeconds, 30)
	operationTimeoutMinutes := int64Default(config.OperationTimeoutMinutes, 60)

	client, err := parasail.New(endpoint, apiKey, time.Duration(requestTimeoutSeconds)*time.Second)
	if err != nil {
		resp.Diagnostics.AddError("Unable to configure Parasail client", err.Error())
		return
	}

	api := &apiConfig{
		Client:           client,
		PollInterval:     time.Duration(pollIntervalSeconds) * time.Second,
		OperationTimeout: time.Duration(operationTimeoutMinutes) * time.Minute,
	}

	resp.DataSourceData = api
	resp.ResourceData = api
}

func (p *parasailProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewDedicatedDeploymentResource,
	}
}

func (p *parasailProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewDedicatedModelSupportDataSource,
		NewDedicatedDeviceConfigsDataSource,
		NewDedicatedDeploymentsDataSource,
	}
}

func int64Default(value types.Int64, fallback int64) int64 {
	if value.IsNull() || value.IsUnknown() || value.ValueInt64() <= 0 {
		return fallback
	}
	return value.ValueInt64()
}
