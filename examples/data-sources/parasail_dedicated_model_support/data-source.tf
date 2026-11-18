data "parasail_dedicated_model_support" "qwen" {
  model_name = "Qwen/Qwen2.5-7B-Instruct"
  engine     = "VLLM"
}

output "qwen_supported" {
  value = data.parasail_dedicated_model_support.qwen.supported
}

output "qwen_engines" {
  value = data.parasail_dedicated_model_support.qwen.supporting_engines
}

output "qwen_messages" {
  value = [for m in data.parasail_dedicated_model_support.qwen.messages : "${m.level}: ${m.content}"]
}
