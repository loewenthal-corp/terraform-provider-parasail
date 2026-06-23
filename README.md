# Terraform Provider for Parasail

This repository contains a Terraform provider for Parasail's public Control API.

The initial implementation focuses on dedicated model deployments using the documented
`/api/v1/dedicated` API surface. It does not depend on Parasail internal code.

## Development

```sh
source bin/activate-hermit
task init
task do
```

Provider configuration can come from Terraform configuration or environment variables:

- `PARASAIL_API_KEY`
- `PARASAIL_ENDPOINT`, defaults to `https://api.parasail.io/api/v1`

### Acceptance Tests

Live acceptance tests use the Terraform provider test harness and create real
Parasail dedicated deployments. Put `PARASAIL_API_KEY` in `.env` or export it in
your shell before running them.

```sh
task test:acc:readonly # read-only support, device, and list checks
task test:acc          # create, update, import, list, and destroy checks
```

The mutating suite uses `wait_for_online = false` to verify dedicated deployment
lifecycle behavior without waiting for instances to finish serving traffic.

## Example

See [examples/basic/main.tf](examples/basic/main.tf).

The resource is intentionally shaped around deployment intent rather than the raw
API payload:

```hcl
resource "parasail_dedicated_deployment" "llama" {
  name  = "llama-prod"
  model = "meta-llama/Llama-3.1-8B-Instruct"

  # Repeat gpu blocks to allow more than one acceptable hardware profile.
  # The API chooses from these selected profiles when provisioning.
  gpu {
    type  = "H100SXM"
    count = 2
  }

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
```
