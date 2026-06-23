data "parasail_dedicated_model_support" "example" {
  model_name = "Qwen/Qwen2.5-7B-Instruct"
  engine     = "VLLM"
}

output "supported" {
  value = data.parasail_dedicated_model_support.example.supported
}
