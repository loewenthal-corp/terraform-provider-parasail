# Autoscaling deployment of an open model on single-GPU replicas.
resource "parasail_dedicated_deployment" "qwen" {
  name        = "qwen-chat"
  model       = "Qwen/Qwen2.5-7B-Instruct"
  description = "Chat assistant backend"
  tags        = ["team:platform", "env:prod"]

  # Repeat gpu blocks to offer alternatives. Parasail provisions each
  # replica on any listed configuration that has capacity.
  gpu {
    type  = "RTX5090"
    count = 1
  }

  gpu {
    type  = "H100SXM"
    count = 1
  }

  autoscaling {
    min_replicas                   = 1
    max_replicas                   = 4
    target_connections_per_replica = 16
  }

  # Scale down after 2 hours idle. Use "never" to keep it running.
  scale_down_after = "2h"
}

# Fixed-size deployment of a gated model, using a Hugging Face token and
# returning as soon as Parasail accepts the request.
variable "hf_token" {
  type      = string
  sensitive = true
}

resource "parasail_dedicated_deployment" "llama" {
  name             = "llama-batch"
  model            = "meta-llama/Llama-3.1-8B-Instruct"
  model_access_key = var.hf_token

  gpu {
    type  = "H100SXM"
    count = 1
  }

  context_length       = 32768
  max_output_tokens    = 4096
  max_request_duration = "10m"
  scale_down_after     = "never"
  wait_for_online      = false
}

# Send requests to base_url with model = model_alias.
output "qwen_endpoint" {
  value = {
    base_url = parasail_dedicated_deployment.qwen.base_url
    model    = parasail_dedicated_deployment.qwen.model_alias
  }
}
