resource "parasail_dedicated_deployment" "example" {
  name  = "qwen-example"
  model = "Qwen/Qwen2.5-7B-Instruct"

  engine = "VLLM"
  mode   = "AUTO"

  # Repeat gpu blocks to offer more than one acceptable hardware profile.
  # Parasail provisions using any selected profile that has capacity.
  gpu {
    type  = "L40S"
    count = 1
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
