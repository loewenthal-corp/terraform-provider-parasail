package provider

import (
	"context"
	"fmt"
	"math"
	"os"
	"sort"
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

const (
	testAccDedicatedModel = "Qwen/Qwen2.5-7B-Instruct"
	testAccNamePrefix     = "tfpacc-"
)

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
					resource.TestCheckResourceAttrSet("data.parasail_dedicated_device_configs.qwen", "configs.0.device"),
					resource.TestCheckResourceAttrSet("data.parasail_dedicated_device_configs.qwen", "configs.0.count"),
					resource.TestCheckResourceAttrSet("data.parasail_dedicated_device_configs.qwen", "configs.0.display_name"),
					testAccCheckDeviceConfigsMatchAPI("data.parasail_dedicated_device_configs.qwen"),
					resource.TestCheckResourceAttrSet("data.parasail_dedicated_deployments.existing", "deployments.#"),
				),
			},
		},
	})
}

// TestAccDedicatedDeployment_multiGPULifecycle creates a deployment that offers
// two GPU types, updates it in place, and verifies that import round-trips
// every attribute Parasail returns.
func TestAccDedicatedDeployment_multiGPULifecycle(t *testing.T) {
	testAccSkipMutating(t)
	testAccPreCheck(t)

	name := testAccName("multi")
	gpus := testAccCheapestSingleGPUs(t, 2)
	testAccCleanupDeployment(t, name)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDedicatedDeploymentDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccDedicatedDeploymentConfig(testAccDeploymentOptions{
					Name:           name,
					GPUs:           gpus,
					ScaleDownAfter: "30m",
					Description:    "Terraform provider acceptance test",
					Tags:           []string{"terraform-provider", "acceptance"},
				}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "name", name),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "model", testAccDedicatedModel),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "engine", "VLLM"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "mode", "AUTO"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "scale_down_after", "30m"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "description", "Terraform provider acceptance test"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "tags.#", "2"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "gpu.#", "2"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "gpu.0.type", gpus[0].Type),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "gpu.0.count", "1"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "gpu.1.type", gpus[1].Type),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "gpu.1.count", "1"),
					resource.TestCheckNoResourceAttr("parasail_dedicated_deployment.test", "autoscaling.min_replicas"),
					resource.TestCheckResourceAttrSet("parasail_dedicated_deployment.test", "id"),
					resource.TestCheckResourceAttrSet("parasail_dedicated_deployment.test", "model_alias"),
					resource.TestCheckResourceAttrSet("parasail_dedicated_deployment.test", "base_url"),
					resource.TestCheckResourceAttrSet("parasail_dedicated_deployment.test", "status"),
					testAccCheckDeploymentListed(name, gpus...),
					testAccCheckRemoteDeployment(name, gpus...),
				),
			},
			{
				Config: testAccDedicatedDeploymentConfig(testAccDeploymentOptions{
					Name:           name,
					GPUs:           gpus,
					ScaleDownAfter: "never",
					Description:    "Terraform provider acceptance test (updated)",
					Tags:           []string{"terraform-provider"},
				}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "scale_down_after", "never"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "description", "Terraform provider acceptance test (updated)"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "tags.#", "1"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "gpu.#", "2"),
					testAccCheckDeploymentListed(name, gpus...),
					testAccCheckRemoteDeployment(name, gpus...),
				),
			},
			{
				ResourceName:      "parasail_dedicated_deployment.test",
				ImportState:       true,
				ImportStateVerify: true,
				// wait_for_online is a provider-side setting that Parasail
				// does not store, so it cannot be recovered on import.
				ImportStateVerifyIgnore: []string{"wait_for_online"},
			},
		},
	})
}

// TestAccDedicatedDeployment_autoscaling creates an autoscaling deployment,
// then removes the autoscaling block and the optional settings to check that
// defaults apply without drift.
func TestAccDedicatedDeployment_autoscaling(t *testing.T) {
	testAccSkipMutating(t)
	testAccPreCheck(t)

	name := testAccName("autoscale")
	gpus := testAccCheapestSingleGPUs(t, 1)
	testAccCleanupDeployment(t, name)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckDedicatedDeploymentDestroy,
		Steps: []resource.TestStep{
			{
				Config: testAccDedicatedDeploymentConfig(testAccDeploymentOptions{
					Name:           name,
					GPUs:           gpus,
					ScaleDownAfter: "30m",
					Autoscaling:    true,
				}),
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
			{
				ResourceName:            "parasail_dedicated_deployment.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"wait_for_online"},
			},
			{
				// Minimal config: engine, mode, and scale_down_after fall back
				// to their defaults, and the deployment becomes fixed-size.
				Config: testAccDedicatedDeploymentConfig(testAccDeploymentOptions{
					Name:    name,
					GPUs:    gpus,
					Minimal: true,
				}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "engine", "VLLM"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "mode", "AUTO"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "scale_down_after", "8h"),
					resource.TestCheckNoResourceAttr("parasail_dedicated_deployment.test", "autoscaling.min_replicas"),
					resource.TestCheckNoResourceAttr("parasail_dedicated_deployment.test", "description"),
					resource.TestCheckNoResourceAttr("parasail_dedicated_deployment.test", "tags.#"),
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

	// Mutating tests query the live catalog before resource.Test runs, so
	// they must skip on their own when acceptance testing is not enabled.
	if os.Getenv(resource.EnvTfAcc) == "" {
		t.Skipf("acceptance tests skipped unless env %q is set", resource.EnvTfAcc)
	}

	if os.Getenv("PARASAIL_ACC_SKIP_MUTATING") == "1" {
		t.Skip("PARASAIL_ACC_SKIP_MUTATING=1 skips live deployment create/update/delete tests")
	}
}

func testAccName(suffix string) string {
	return fmt.Sprintf("%s%s-%d", testAccNamePrefix, suffix, time.Now().UTC().Unix())
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

type testAccDeploymentOptions struct {
	Name           string
	GPUs           []testAccGPU
	ScaleDownAfter string
	Description    string
	Tags           []string
	Autoscaling    bool
	// Minimal omits every optional argument except wait_for_online.
	Minimal bool
}

func testAccDedicatedDeploymentConfig(opts testAccDeploymentOptions) string {
	var b strings.Builder

	b.WriteString(testAccProviderConfig())
	b.WriteString("\nresource \"parasail_dedicated_deployment\" \"test\" {\n")
	fmt.Fprintf(&b, "  name            = %q\n", opts.Name)
	fmt.Fprintf(&b, "  model           = %q\n", testAccDedicatedModel)
	b.WriteString("  wait_for_online = false\n")

	if !opts.Minimal {
		b.WriteString("  engine          = \"VLLM\"\n")
		b.WriteString("  mode            = \"AUTO\"\n")
		if opts.ScaleDownAfter != "" {
			fmt.Fprintf(&b, "  scale_down_after = %q\n", opts.ScaleDownAfter)
		}
		if opts.Description != "" {
			fmt.Fprintf(&b, "  description     = %q\n", opts.Description)
		}
		if len(opts.Tags) > 0 {
			quoted := make([]string, 0, len(opts.Tags))
			for _, tag := range opts.Tags {
				quoted = append(quoted, strconv.Quote(tag))
			}
			fmt.Fprintf(&b, "  tags            = [%s]\n", strings.Join(quoted, ", "))
		}
	}

	for _, gpu := range opts.GPUs {
		fmt.Fprintf(&b, "\n  gpu {\n    type  = %q\n    count = %d\n  }\n", gpu.Type, gpu.Count)
	}

	if opts.Autoscaling && !opts.Minimal {
		b.WriteString(`
  autoscaling {
    min_replicas                   = 1
    max_replicas                   = 1
    target_connections_per_replica = 32
    smoothing_factor               = 0.3
  }
`)
	}

	b.WriteString(`}

data "parasail_dedicated_deployments" "after" {
  depends_on = [parasail_dedicated_deployment.test]
}
`)
	return b.String()
}

// testAccCheapestSingleGPUs returns the n cheapest single-GPU configurations
// Parasail currently offers for the test model, each a different GPU type.
// Picking from the live catalog keeps the suite working when Parasail retires
// or adds GPU types, and single-GPU configs keep the cost of a run low.
func testAccCheapestSingleGPUs(t *testing.T, n int) []testAccGPU {
	t.Helper()

	client, err := testAccClient()
	if err != nil {
		t.Fatal(err)
	}

	configs, err := client.DeviceConfigs(context.Background(), parasail.DeviceConfigsRequest{
		Engine:    "VLLM",
		ModelName: testAccDedicatedModel,
	})
	if err != nil {
		t.Fatalf("listing device configs for %s: %v", testAccDedicatedModel, err)
	}

	gpus := testAccSelectCheapestSingleGPUs(configs, n)
	if len(gpus) < n {
		t.Skipf("Parasail offers %d single-GPU types for %s, need %d", len(gpus), testAccDedicatedModel, n)
	}
	t.Logf("using GPU configs %v", gpus)
	return gpus
}

func testAccSelectCheapestSingleGPUs(configs []parasail.DedicatedDeviceConfig, n int) []testAccGPU {
	candidates := make([]parasail.DedicatedDeviceConfig, 0, len(configs))
	for _, config := range configs {
		if config.Count == 1 && config.Device != "" {
			candidates = append(candidates, config)
		}
	}

	cost := func(config parasail.DedicatedDeviceConfig) float64 {
		if config.Cost == nil {
			return math.MaxFloat64
		}
		return *config.Cost
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if cost(candidates[i]) != cost(candidates[j]) {
			return cost(candidates[i]) < cost(candidates[j])
		}
		return candidates[i].Device < candidates[j].Device
	})

	seen := map[string]bool{}
	out := make([]testAccGPU, 0, n)
	for _, config := range candidates {
		if len(out) == n {
			break
		}
		if seen[config.Device] {
			continue
		}
		seen[config.Device] = true
		out = append(out, testAccGPU{Type: config.Device, Count: config.Count})
	}
	return out
}

// testAccCleanupDeployment deletes a leftover test deployment by exact name
// after the test, in case a failed step left it outside Terraform state. It
// only ever touches the deployment created by this test.
func testAccCleanupDeployment(t *testing.T, name string) {
	t.Helper()

	if !strings.HasPrefix(name, testAccNamePrefix) {
		t.Fatalf("refusing to register cleanup for non-test deployment %q", name)
	}

	t.Cleanup(func() {
		client, err := testAccClient()
		if err != nil {
			t.Logf("cleanup: %v", err)
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		deployment, err := testAccFindRemoteDeployment(ctx, client, name)
		if err != nil {
			return
		}
		t.Logf("cleanup: deleting leftover deployment %s (%d)", name, deployment.ID)
		if err := client.DeleteDeployment(ctx, deployment.ID); err != nil && !parasail.IsNotFound(err) {
			t.Errorf("cleanup: deleting leftover deployment %s: %v", name, err)
		}
	})
}

func testAccCheckDeviceConfigsMatchAPI(address string) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		attrs, err := testAccResourceAttrs(s, address)
		if err != nil {
			return err
		}

		client, err := testAccClient()
		if err != nil {
			return err
		}
		configs, err := client.DeviceConfigs(context.Background(), parasail.DeviceConfigsRequest{
			Engine:    "VLLM",
			ModelName: testAccDedicatedModel,
		})
		if err != nil {
			return err
		}

		for _, config := range configs {
			gpu := testAccGPU{Type: config.Device, Count: config.Count}
			if !testAccFindFlatmapGPU(attrs, "configs", gpu) {
				return fmt.Errorf("%s did not include device config %s x%d returned by the API", address, gpu.Type, gpu.Count)
			}
		}
		return nil
	}
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
