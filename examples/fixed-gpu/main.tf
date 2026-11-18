# Deploys a gated Llama model as a fixed-size deployment (one replica, no
# autoscaling) that never scales down. Either GPU configuration is acceptable;
# Parasail uses whichever has capacity.

terraform {
  required_version = ">= 1.6.0"

  required_providers {
    parasail = {
      source  = "registry.terraform.io/loewenthal-corp/parasail"
      version = ">= 0.2.0"
    }
  }
}

provider "parasail" {}

variable "hf_token" {
  description = "Hugging Face token with access to meta-llama/Llama-3.1-8B-Instruct."
  type        = string
  sensitive   = true
}

resource "parasail_dedicated_deployment" "llama" {
  name             = "llama-prod"
  model            = "meta-llama/Llama-3.1-8B-Instruct"
  model_access_key = var.hf_token

  gpu {
    type  = "H100SXM"
    count = 1
  }

  gpu {
    type  = "H200SXM"
    count = 1
  }

  max_connections_per_replica = 64
  scale_down_after            = "never"
}

output "model_alias" {
  value = parasail_dedicated_deployment.llama.model_alias
}
