data "parasail_dedicated_device_configs" "qwen" {
  model_name = "Qwen/Qwen2.5-7B-Instruct"
  engine     = "VLLM"
}

locals {
  # Single-GPU configurations, preferring ones with capacity right now.
  single_gpu = [for c in data.parasail_dedicated_device_configs.qwen.configs : c if c.count == 1]
  candidates = length([for c in local.single_gpu : c if c.available]) > 0 ? [for c in local.single_gpu : c if c.available] : local.single_gpu
  cheapest   = [for c in local.candidates : c if c.cost == min([for x in local.candidates : x.cost]...)][0]
}

resource "parasail_dedicated_deployment" "qwen" {
  name  = "qwen-cheapest"
  model = data.parasail_dedicated_device_configs.qwen.model_name

  gpu {
    type  = local.cheapest.device
    count = local.cheapest.count
  }
}

output "price_per_hour" {
  value = local.cheapest.cost
}
