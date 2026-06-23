terraform {
  required_version = ">= 1.6.0"

  required_providers {
    parasail = {
      source  = "registry.terraform.io/loewenthal-corp/parasail"
      version = ">= 0.1.0"
    }
  }
}

provider "parasail" {}

resource "parasail_dedicated_deployment" "llama" {
  name  = "llama-prod"
  model = "meta-llama/Llama-3.1-8B-Instruct"

  gpu {
    type  = "H100SXM"
    count = 2
  }

  # Optional alternate profile. Parasail can choose any selected profile
  # that satisfies the deployment and available capacity.
  gpu {
    type  = "A100_80GB"
    count = 4
  }

  autoscaling {
    min_replicas = 1
    max_replicas = 5
  }

  scale_down_after = "never"
}

output "model_alias" {
  value = parasail_dedicated_deployment.llama.model_alias
}
