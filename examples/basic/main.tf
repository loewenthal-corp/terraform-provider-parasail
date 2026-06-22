terraform {
  required_providers {
    parasail = {
      source = "loewenthal-corp/parasail"
    }
  }
}

provider "parasail" {
  # api_key may also be set with PARASAIL_API_KEY.
  # endpoint defaults to https://api.parasail.io/api/v1.
}

data "parasail_dedicated_model_support" "example" {
  model_name = "Qwen/Qwen2.5-7B-Instruct"
  engine     = "VLLM"
}

data "parasail_dedicated_device_configs" "example" {
  model_name = data.parasail_dedicated_model_support.example.model_name
  engine     = "VLLM"
}

resource "parasail_dedicated_deployment" "example" {
  name  = "qwen-example"
  model = data.parasail_dedicated_model_support.example.model_name

  engine = "VLLM"
  mode   = "AUTO"

  gpu {
    type  = data.parasail_dedicated_device_configs.example.configs[0].device
    count = data.parasail_dedicated_device_configs.example.configs[0].count
  }

  autoscaling {
    min_replicas                   = 1
    max_replicas                   = 5
    target_connections_per_replica = 16
  }

  scale_down_after = "8h"
}

output "model_alias" {
  value = parasail_dedicated_deployment.example.model_alias
}

output "status" {
  value = parasail_dedicated_deployment.example.status
}
