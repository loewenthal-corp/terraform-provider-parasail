# Deploys Qwen2.5-7B-Instruct on the cheapest single-GPU configuration
# Parasail currently offers for it, with autoscaling and idle scale-down.

terraform {
  required_version = ">= 1.6.0"

  required_providers {
    parasail = {
      source  = "registry.terraform.io/loewenthal-corp/parasail"
      version = ">= 0.2.0"
    }
  }
}

provider "parasail" {
  # api_key defaults to the PARASAIL_API_KEY environment variable.
}

data "parasail_dedicated_model_support" "qwen" {
  model_name = "Qwen/Qwen2.5-7B-Instruct"
  engine     = "VLLM"

  lifecycle {
    postcondition {
      condition     = self.supported
      error_message = "Parasail cannot deploy ${self.model_name}: ${join("; ", [for m in self.messages : m.content])}"
    }
  }
}

data "parasail_dedicated_device_configs" "qwen" {
  model_name = data.parasail_dedicated_model_support.qwen.model_name
  engine     = "VLLM"
}

locals {
  # Single-GPU configurations, preferring ones with capacity right now.
  single_gpu = [for c in data.parasail_dedicated_device_configs.qwen.configs : c if c.count == 1]
  candidates = length([for c in local.single_gpu : c if c.available]) > 0 ? [for c in local.single_gpu : c if c.available] : local.single_gpu
  cheapest   = [for c in local.candidates : c if c.cost == min([for x in local.candidates : x.cost]...)][0]
}

resource "parasail_dedicated_deployment" "qwen" {
  name  = "qwen-example"
  model = data.parasail_dedicated_model_support.qwen.model_name

  gpu {
    type  = local.cheapest.device
    count = local.cheapest.count
  }

  autoscaling {
    min_replicas                   = 1
    max_replicas                   = 3
    target_connections_per_replica = 16
  }

  scale_down_after = "1h"
}

output "gpu" {
  value = local.cheapest.display_name
}

output "base_url" {
  value = parasail_dedicated_deployment.qwen.base_url
}

output "model_alias" {
  value = parasail_dedicated_deployment.qwen.model_alias
}
