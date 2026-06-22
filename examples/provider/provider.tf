provider "parasail" {
  api_key = var.parasail_api_key
}

variable "parasail_api_key" {
  type      = string
  sensitive = true
}

