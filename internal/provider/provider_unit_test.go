package provider

import (
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	parasail "github.com/loewenthal-corp/terraform-provider-parasail/internal/client"
)

// These tests drive the provider through the real Terraform CLI against the
// in-memory fake API, so they need a terraform binary (hermit provides one)
// but no network access or Parasail API key.

func requireTerraform(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("terraform binary not on PATH; run `source bin/activate-hermit`")
	}
}

func fakeProviderConfig(f *fakeParasail) string {
	return fmt.Sprintf(`
provider "parasail" {
  api_key                   = "test-key"
  endpoint                  = %q
  poll_interval_seconds     = 1
  operation_timeout_minutes = 1
}
`, f.endpoint())
}

func TestUnitDedicatedDeployment_lifecycle(t *testing.T) {
	requireTerraform(t)
	fake := newFakeParasail(t)
	fake.setStatuses("STARTING", "ONLINE")

	full := fakeProviderConfig(fake) + `
resource "parasail_dedicated_deployment" "test" {
  name             = "qwen-unit"
  model            = "Qwen/Qwen2.5-7B-Instruct"
  model_access_key = "hf_secret"
  description      = "unit"
  tags             = ["b", "a"]
  scale_down_after = "480m"

  max_request_duration = "5m"
  max_output_tokens    = 1024

  gpu {
    type  = "RTX5090"
    count = 1
  }

  gpu {
    type  = "H100SXM"
    count = 1
  }

  autoscaling {
    max_replicas = 3
  }
}
`
	updated := fakeProviderConfig(fake) + `
resource "parasail_dedicated_deployment" "test" {
  name             = "qwen-unit"
  model            = "Qwen/Qwen2.5-7B-Instruct"
  model_access_key = "hf_secret"
  scale_down_after = "never"
  wait_for_online  = false

  gpu {
    type  = "H100SXM"
    count = 2
  }
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if n := fake.count(); n != 0 {
				return fmt.Errorf("%d deployments left after destroy", n)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: full,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "status", "ONLINE"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "engine", "VLLM"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "mode", "AUTO"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "scale_down_after", "480m"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "max_request_duration", "5m"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "max_output_tokens", "1024"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "model_alias", "acct-qwen-unit"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "base_url", "https://api.parasail.io/v1"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "autoscaling.min_replicas", "1"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "autoscaling.max_replicas", "3"),
					resource.TestCheckNoResourceAttr("parasail_dedicated_deployment.test", "autoscaling.smoothing_factor"),
					resource.TestCheckNoResourceAttr("parasail_dedicated_deployment.test", "context_length"),
					func(*terraform.State) error {
						d := fake.only()
						if d == nil {
							return fmt.Errorf("deployment not created")
						}
						if d.ScaleDownPolicy != "TIMER" || d.ScaleDownThreshold != 28800000 {
							return fmt.Errorf("scale down sent as %s/%d", d.ScaleDownPolicy, d.ScaleDownThreshold)
						}
						if d.Autoscaling == nil || !*d.Autoscaling || *d.MinReplicas != 1 || *d.MaxReplicas != 3 {
							return fmt.Errorf("autoscaling sent as %v", d)
						}
						return nil
					},
				),
			},
			{
				Config: updated,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "scale_down_after", "never"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "gpu.#", "1"),
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "gpu.0.type", "H100SXM"),
					resource.TestCheckNoResourceAttr("parasail_dedicated_deployment.test", "autoscaling.min_replicas"),
					resource.TestCheckNoResourceAttr("parasail_dedicated_deployment.test", "description"),
					resource.TestCheckNoResourceAttr("parasail_dedicated_deployment.test", "tags.#"),
					// wait_for_online = false returns before the deployment is ONLINE.
					resource.TestCheckResourceAttr("parasail_dedicated_deployment.test", "status", "STARTING"),
				),
			},
			{
				ResourceName:            "parasail_dedicated_deployment.test",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"wait_for_online", "model_access_key", "status"},
			},
			{
				ResourceName:  "parasail_dedicated_deployment.test",
				ImportState:   true,
				ImportStateId: "not-a-number",
				ExpectError:   regexp.MustCompile(`Expected a numeric Parasail deployment ID`),
			},
		},
	})
}

func TestUnitDedicatedDeployment_failedWaitIsTracked(t *testing.T) {
	requireTerraform(t)
	fake := newFakeParasail(t)
	fake.setStatuses("STARTING", "ERROR")

	config := fakeProviderConfig(fake) + `
resource "parasail_dedicated_deployment" "test" {
  name  = "qwen-error"
  model = "Qwen/Qwen2.5-7B-Instruct"

  gpu {
    type  = "RTX5090"
    count = 1
  }
}
`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// The failed create must still be in state (tainted), so the
		// test's destroy removes it instead of leaking a deployment.
		CheckDestroy: func(*terraform.State) error {
			if n := fake.count(); n != 0 {
				return fmt.Errorf("%d deployments leaked after a failed create", n)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config:      config,
				ExpectError: regexp.MustCompile(`deployment entered ERROR: out of capacity`),
			},
		},
	})
}

func TestUnitDedicatedDeployment_driftAndRemoval(t *testing.T) {
	requireTerraform(t)
	fake := newFakeParasail(t)

	config := fakeProviderConfig(fake) + `
resource "parasail_dedicated_deployment" "test" {
  name             = "qwen-drift"
  model            = "Qwen/Qwen2.5-7B-Instruct"
  description      = "managed"
  scale_down_after = "30m"
  wait_for_online  = false

  gpu {
    type  = "RTX5090"
    count = 1
  }
}
`
	var id int64

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: func(s *terraform.State) error {
					var err error
					id, err = strconv.ParseInt(s.RootModule().Resources["parasail_dedicated_deployment.test"].Primary.ID, 10, 64)
					return err
				},
			},
			{
				// Out-of-band edits show up as an in-place update.
				PreConfig: func() {
					fake.mutate(id, func(d *parasail.DedicatedDeployment) {
						d.Description = "edited in the console"
						d.ScaleDownThreshold = 3600000
					})
				},
				Config: config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("parasail_dedicated_deployment.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: func(*terraform.State) error {
					if d := fake.get(id); d == nil || d.Description != "managed" || d.ScaleDownThreshold != 1800000 {
						return fmt.Errorf("drift was not corrected: %#v", d)
					}
					return nil
				},
			},
			{
				// A deployment deleted outside Terraform is recreated.
				PreConfig: func() { fake.remove(id) },
				Config:    config,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("parasail_dedicated_deployment.test", plancheck.ResourceActionCreate),
					},
				},
			},
		},
	})
}

func TestUnitDedicatedDeployment_planTimeValidation(t *testing.T) {
	requireTerraform(t)
	fake := newFakeParasail(t)

	tests := []struct {
		name  string
		body  string
		error string
	}{
		{
			name:  "name too long",
			body:  `name = "this-name-is-definitely-longer-than-32"`,
			error: `string length must be between 2 and 32`,
		},
		{
			name:  "name uppercase",
			body:  `name = "Qwen"`,
			error: `must contain only lowercase letters, numbers, and hyphens`,
		},
		{
			name:  "bad scale_down_after",
			body:  "name = \"qwen\"\n  scale_down_after = \"1d\"",
			error: `scale_down_after must be a non-negative Go duration`,
		},
		{
			name:  "bad max_request_duration",
			body:  "name = \"qwen\"\n  max_request_duration = \"0s\"",
			error: `must be a positive Go duration`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config: fakeProviderConfig(fake) + fmt.Sprintf(`
resource "parasail_dedicated_deployment" "test" {
  %s
  model = "Qwen/Qwen2.5-7B-Instruct"

  gpu {
    type  = "RTX5090"
    count = 1
  }
}
`, tt.body),
						PlanOnly:    true,
						ExpectError: regexp.MustCompile(tt.error),
					},
				},
			})
		})
	}

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fakeProviderConfig(fake) + `
resource "parasail_dedicated_deployment" "test" {
  name  = "qwen"
  model = "Qwen/Qwen2.5-7B-Instruct"
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`(?s)gpu.*must have a configuration value`),
			},
		},
	})

	if n := fake.count(); n != 0 {
		t.Fatalf("validation failures created %d deployments", n)
	}
}

func TestUnitDataSources(t *testing.T) {
	requireTerraform(t)
	fake := newFakeParasail(t)

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fakeProviderConfig(fake) + `
resource "parasail_dedicated_deployment" "test" {
  name            = "qwen-listed"
  model           = "Qwen/Qwen2.5-7B-Instruct"
  wait_for_online = false

  gpu {
    type  = "RTX5090"
    count = 1
  }

  autoscaling {
    min_replicas = 1
    max_replicas = 2
  }
}

data "parasail_dedicated_model_support" "qwen" {
  model_name = "Qwen/Qwen2.5-7B-Instruct"
  engine     = "VLLM"
}

data "parasail_dedicated_model_support" "unknown" {
  model_name = "unknown/model"
}

data "parasail_dedicated_device_configs" "qwen" {
  model_name = "Qwen/Qwen2.5-7B-Instruct"
}

data "parasail_dedicated_deployments" "all" {
  depends_on = [parasail_dedicated_deployment.test]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.parasail_dedicated_model_support.qwen", "supported", "true"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_model_support.qwen", "known", "true"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_model_support.qwen", "supporting_engines.0", "VLLM"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_model_support.qwen", "messages.0.level", "INFO"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_model_support.unknown", "supported", "false"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_model_support.unknown", "known", "false"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_device_configs.qwen", "configs.#", "3"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_device_configs.qwen", "configs.0.device", "RTX5090"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_device_configs.qwen", "configs.0.cost", "0.65"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_device_configs.qwen", "configs.0.recommended", "true"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_device_configs.qwen", "configs.0.available", "false"),
					resource.TestCheckNoResourceAttr("data.parasail_dedicated_device_configs.qwen", "configs.0.estimated_system_tps"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_device_configs.qwen", "configs.2.display_name", "H100SXM x 2"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_deployments.all", "deployments.#", "1"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_deployments.all", "deployments.0.name", "qwen-listed"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_deployments.all", "deployments.0.model_alias", "acct-qwen-listed"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_deployments.all", "deployments.0.engine", "VLLM"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_deployments.all", "deployments.0.mode", "AUTO"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_deployments.all", "deployments.0.autoscaling", "true"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_deployments.all", "deployments.0.max_replicas", "2"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_deployments.all", "deployments.0.gpu.0.type", "RTX5090"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_deployments.all", "deployments.0.gpu.0.selected", "true"),
					resource.TestCheckResourceAttr("data.parasail_dedicated_deployments.all", "deployments.0.status", "STARTING"),
				),
			},
		},
	})
}

func TestUnitProviderConfiguration(t *testing.T) {
	requireTerraform(t)
	t.Setenv("PARASAIL_API_KEY", "")
	t.Setenv("PARASAIL_ENDPOINT", "")

	tests := []struct {
		name   string
		config string
		error  string
	}{
		{
			name:   "missing api key",
			config: `provider "parasail" {}`,
			error:  `Missing Parasail API key`,
		},
		{
			name: "relative endpoint",
			config: `provider "parasail" {
  api_key  = "test-key"
  endpoint = "/api/v1"
}`,
			error: `endpoint must be an absolute URL`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource.UnitTest(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{
						Config:      tt.config + "\n\ndata \"parasail_dedicated_deployments\" \"all\" {}\n",
						ExpectError: regexp.MustCompile(tt.error),
					},
				},
			})
		})
	}

	t.Run("environment", func(t *testing.T) {
		fake := newFakeParasail(t)
		t.Setenv("PARASAIL_API_KEY", "test-key")
		t.Setenv("PARASAIL_ENDPOINT", fake.endpoint())

		resource.UnitTest(t, resource.TestCase{
			ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
			Steps: []resource.TestStep{
				{
					Config: `
provider "parasail" {}

data "parasail_dedicated_deployments" "all" {}
`,
					Check: resource.TestCheckResourceAttr("data.parasail_dedicated_deployments.all", "deployments.#", "0"),
				},
			},
		})
	})
}
