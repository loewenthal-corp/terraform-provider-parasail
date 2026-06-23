terraform {
  required_version = ">= 1.6.0"

  required_providers {
    parasail = {
      source  = "registry.terraform.io/loewenthal-corp/parasail"
      version = ">= 0.1.0"
    }
  }
}

provider "parasail" {
  api_key = var.parasail_api_key
}

variable "parasail_api_key" {
  type      = string
  sensitive = true
}
