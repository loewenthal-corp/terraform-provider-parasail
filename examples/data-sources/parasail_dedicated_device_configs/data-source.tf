data "parasail_dedicated_device_configs" "example" {
  model_name = "Qwen/Qwen2.5-7B-Instruct"
  engine     = "VLLM"
}

output "first_device" {
  value = data.parasail_dedicated_device_configs.example.configs[0].device
}
