package provider

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	parasail "github.com/loewenthal-corp/terraform-provider-parasail/internal/client"
)

const testAccDedicatedModel = "Qwen/Qwen2.5-7B-Instruct"

var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"parasail": providerserver.NewProtocol6WithError(New("test")()),
}

type testAccGPU struct {
	Type  string
	Count int64
}

func TestAccDedicatedDataSources_readOnly(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccDedicatedDataSourcesConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.parasail_dedicated_model_support.qwen", "known", "true"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_model_support.qwen", "supported", "true"),
					testAccCheckPositiveCount("data.parasail_dedicated_model_support.qwen", "supporting_engines.#"),
					testAccCheckPositiveCount("data.parasail_dedicated_device_configs.qwen", "configs.#"),
					testAccCheckDeviceConfigsContain(
						"data.parasail_dedicated_device_configs.qwen",
						testAccGPU{Type: "L40S", Count: 1},
						testAccGPU{Type: "RTX4090", Count: 1},
					),
					resource.TestCheckResourceAttrSet("data.parasail_dedicated_deployments.existing", "deployments.#"),
				),
			},
		},
	})
}

func TestAccDedicatedDeployment_multiGPULifecycle(t *testing.T) {
	testAccSkipMutating(t)

	name := testAccName("multi")
	gpus := []testAccGPU{
		{Type: "L40S", Count: 1},
		{Type: "RTX4090", Count: 1},
	}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDedicatedDeploymentDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccDedicatedDeploymentConfig(name, "30m", false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "name", name),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "model", testAccDedicatedModel),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "engine", "VLLM"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "mode", "AUTO"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "scale_down_after", "30m"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "gpu.#", "2"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "gpu.0.type", "L40S"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "gpu.0.count", "1"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "gpu.1.type", "RTX4090"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "gpu.1.count", "1"),
					resource.TestCheckResourceAttrSet("parasail_dedicated_deployment.test", "id"),
					resource.TestCheckResourceAttrSet("parasail_dedicated_deployment.test", "model_alias"),
					resource.TestCheckResourceAttrSet("parasail_dedicated_deployment.test", "base_url"),
					testAccCheckDeploymentListed(name, gpus...),
					testAccCheckRemoteDeployment(name, gpus...),
				),
			},
			{
				Config: testAccDedicatedDeploymentConfig(name, "never", false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "scale_down_after", "never"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "gpu.#", "2"),
					testAccCheckDeploymentListed(name, gpus...),
					testAccCheckRemoteDeployment(name, gpus...),
				),
			},
			{
				ResourceName:      "parasail_dedicated_deployment.test",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"autoscaling",
					"chat_template",
					"context_length",
					"deployment_available_at",
					"description",
					"draft_model",
					"draft_model_access_key",
					"engine",
					"gpu",
					"max_connections_per_replica",
					"max_output_tokens",
					"max_request_duration",
					"mode",
					"model_access_key",
					"status",
					"status_message",
					"tags",
					"wait_for_online",
				},
			},
		},
	})
}

func TestAccDedicatedDeployment_autoscaling(t *testing.T) {
	testAccSkipMutating(t)

	name := testAccName("autoscale")
	gpus := []testAccGPU{{Type: "L40S", Count: 1}}

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDedicatedDeploymentDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccDedicatedDeploymentConfig(name, "30m", true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "name", name),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "autoscaling.min_replicas", "1"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "autoscaling.max_replicas", "1"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "autoscaling.target_connections_per_replica", "32"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "autoscaling.smoothing_factor", "0.3"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "gpu.#", "1"),
					resource.TestCheckResourceAttrSet("parasail_dedicated_deployment.test", "id"),
					testAccCheckRemoteDeployment(name, gpus...),
				),
			},
		},
	})
}

func testAccPreCheck(t *testing.T) {
	t.Helper()

	if os.Getenv("PARASAIL_API_KEY") == "" {
		t.Fatal("PARASAIL_API_KEY must be set for acceptance tests")
	}
}

func testAccSkipMutating(t *testing.T) {
	t.Helper()

	if os.Getenv("PARASAIL_ACC_SKIP_MUTATING") == "1" {
		t.Skip("PARASAIL_ACC_SKIP_MUTATING=1 skips live deployment create/update/delete tests")
	}
}

func testAccName(suffix string) string {
	return fmt.Sprintf("tfpacc-%s-%d", suffix, time.Now().UTC().Unix())
}

func testAccProviderConfig() string {
	return `
provider "parasail" {
  request_timeout_seconds   = 60
  poll_interval_seconds     = 5
  operation_timeout_minutes = 30
}
`
}

func testAccDedicatedDataSourcesConfig() string {
	return fmt.Sprintf(`
%s

data "parasail_dedicated_model_support" "qwen" {
  model_name = %[2]q
  engine     = "VLLM"
}

data "parasail_dedicated_device_configs" "qwen" {
  model_name = data.parasail_dedicated_model_support.qwen.model_name
  engine     = "VLLM"
}

data "parasail_dedicated_deployments" "existing" {}
`, testAccProviderConfig(), testAccDedicatedModel)
}

func testAccDedicatedDeploymentConfig(name string, scaleDownAfter string, autoscaling bool) string {
	gpuBlocks := `
  gpu {
    type  = "L40S"
    count = 1
  }
`
	if !autoscaling {
		gpuBlocks += `
  gpu {
    type  = "RTX4090"
    count = 1
  }
`
	}

	autoscalingBlock := ""
	if autoscaling {
		autoscalingBlock = `
  autoscaling {
    min_replicas                   = 1
    max_replicas                   = 1
    target_connections_per_replica = 32
    smoothing_factor               = 0.3
  }
`
	}

	return fmt.Sprintf(`
%s

resource "parasail_dedicated_deployment" "test" {
  name             = %[2]q
  model            = %[3]q
  engine           = "VLLM"
  mode             = "AUTO"
  description      = "Terraform provider acceptance test"
  tags             = ["terraform-provider", "acceptance"]
  scale_down_after = %[4]q
  wait_for_online  = false
%s%s
}

data "parasail_dedicated_deployments" "after" {
  depends_on = [parasail_dedicated_deployment.test]
}
`, testAccProviderConfig(), name, testAccDedicatedModel, scaleDownAfter, gpuBlocks, autoscalingBlock)
}

func testAccCheckPositiveCount(address string, key string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		value, err := testAccResourceAttr(s, address, key)
		if err != nil {
			return err
		}

		count, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("%s.%s value %q is not an integer: %w", address, key, value, err)
		}
		if count <= 0 {
			return fmt.Errorf("%s.%s is %d, want a positive count", address, key, count)
		}
		return nil
	}
}

func testAccCheckDeviceConfigsContain(address string, want ...testAccGPU) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		attrs, err := testAccResourceAttrs(s, address)
		if err != nil {
			return err
		}

		for _, gpu := range want {
			if !testAccFindFlatmapGPU(attrs, "configs", gpu) {
				return fmt.Errorf("%s did not include device config %s x%d", address, gpu.Type, gpu.Count)
			}
		}
		return nil
	}
}

func testAccCheckDeploymentListed(name string, want ...testAccGPU) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		attrs, err := testAccResourceAttrs(s, "data.parasail_dedicated_deployments.after")
		if err != nil {
			return err
		}

		count, err := strconv.Atoi(attrs["deployments.#"])
		if err != nil {
			return fmt.Errorf("deployments.# value %q is not an integer: %w", attrs["deployments.#"], err)
		}

		for i := range count {
			prefix := fmt.Sprintf("deployments.%d", i)
			if attrs[prefix+".name"] != name {
				continue
			}
			for _, gpu := range want {
				if !testAccFindFlatmapGPU(attrs, prefix+".gpu", gpu) {
					return fmt.Errorf("listed deployment %s did not include GPU %s x%d", name, gpu.Type, gpu.Count)
				}
			}
			return nil
		}

		return fmt.Errorf("data.parasail_dedicated_deployments.after did not include deployment %s", name)
	}
}

func testAccCheckRemoteDeployment(name string, want ...testAccGPU) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		client, err := testAccClient()
		if err != nil {
			return err
		}

		deployment, err := testAccFindRemoteDeployment(context.Background(), client, name)
		if err != nil {
			return err
		}

		for _, gpu := range want {
			if !testAccRemoteDeploymentHasGPU(deployment, gpu) {
				return fmt.Errorf("remote deployment %s did not include GPU %s x%d", name, gpu.Type, gpu.Count)
			}
		}
		return nil
	}
}

func testAccCheckDedicatedDeploymentDestroy(s *terraform.State) error {
	client, err := testAccClient()
	if err != nil {
		return err
	}

	for _, rs := range s.RootModule().Resources {
		if rs.Type != "parasail_dedicated_deployment" {
			continue
		}
		if rs.Primary == nil || rs.Primary.ID == "" {
			continue
		}

		id, err := strconv.ParseInt(rs.Primary.ID, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid deployment ID %q in state: %w", rs.Primary.ID, err)
		}

		deployments, err := client.ListDeployments(context.Background())
		if err != nil {
			return fmt.Errorf("listing deployments after destroy: %w", err)
		}
		for _, deployment := range deployments {
			if deployment.ID == id {
				return fmt.Errorf("deployment %d still exists after destroy", id)
			}
		}
	}

	return nil
}

func testAccResourceAttrs(s *terraform.State, address string) (map[string]string, error) {
	resourceState, ok := s.RootModule().Resources[address]
	if !ok {
		return nil, fmt.Errorf("%s not found in Terraform state", address)
	}
	if resourceState.Primary == nil {
		return nil, fmt.Errorf("%s has no primary state", address)
	}
	return resourceState.Primary.Attributes, nil
}

func testAccResourceAttr(s *terraform.State, address string, key string) (string, error) {
	attrs, err := testAccResourceAttrs(s, address)
	if err != nil {
		return "", err
	}

	value, ok := attrs[key]
	if !ok {
		return "", fmt.Errorf("%s.%s not found in Terraform state", address, key)
	}
	return value, nil
}

func testAccFindFlatmapGPU(attrs map[string]string, prefix string, gpu testAccGPU) bool {
	count, err := strconv.Atoi(attrs[prefix+".#"])
	if err != nil {
		return false
	}

	for i := range count {
		itemPrefix := fmt.Sprintf("%s.%d", prefix, i)
		if attrs[itemPrefix+".type"] == gpu.Type && attrs[itemPrefix+".count"] == strconv.FormatInt(gpu.Count, 10) {
			return true
		}
		if attrs[itemPrefix+".device"] == gpu.Type && attrs[itemPrefix+".count"] == strconv.FormatInt(gpu.Count, 10) {
			return true
		}
	}

	return false
}

func testAccClient() (*parasail.Client, error) {
	endpoint := os.Getenv("PARASAIL_ENDPOINT")
	if endpoint == "" {
		endpoint = parasail.DefaultEndpoint
	}

	client, err := parasail.New(endpoint, os.Getenv("PARASAIL_API_KEY"), time.Minute)
	if err != nil {
		return nil, fmt.Errorf("creating acceptance test client: %w", err)
	}
	return client, nil
}

func testAccFindRemoteDeployment(ctx context.Context, client *parasail.Client, name string) (parasail.DedicatedDeployment, error) {
	deployments, err := client.ListDeployments(ctx)
	if err != nil {
		return parasail.DedicatedDeployment{}, fmt.Errorf("listing deployments: %w", err)
	}

	for _, deployment := range deployments {
		if deployment.DeploymentName == name || deployment.DisplayName == name {
			return deployment, nil
		}
	}

	return parasail.DedicatedDeployment{}, fmt.Errorf("deployment %s not found in Parasail deployment list", name)
}

func testAccRemoteDeploymentHasGPU(deployment parasail.DedicatedDeployment, gpu testAccGPU) bool {
	for _, config := range deployment.DeviceConfigs {
		if strings.EqualFold(config.Device, gpu.Type) && config.Count == gpu.Count {
			return true
		}
	}
	return false
}
