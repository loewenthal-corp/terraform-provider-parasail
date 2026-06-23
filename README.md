# Terraform Provider for Parasail (Unofficial)

> **Unofficial / community project.** This provider is not built, maintained, or
> endorsed by Parasail. It is an independent project that talks to Parasail's
> **public** Control API only, and depends on no Parasail internal code. "Parasail"
> is used solely to describe the API the provider targets.

This repository contains a Terraform (and OpenTofu) provider for Parasail's public
Control API. The initial implementation focuses on dedicated model deployments using
the documented `/api/v1/dedicated` API surface.

Once published, the provider is sourced as `loewenthal-corp/parasail`:

```hcl
terraform {
  required_providers {
    parasail = {
      source  = "loewenthal-corp/parasail"
      version = ">= 0.1.0"
    }
  }
}
```

Generated provider documentation lives in [docs/](docs/) and is published on the
Terraform Registry.

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

## Documentation

Registry docs under [docs/](docs/) are generated from the provider schema and the
files in [examples/](examples/) with
[`tfplugindocs`](https://github.com/hashicorp/terraform-plugin-docs):

```sh
task docs           # regenerate docs/ and validate them
task docs:validate  # validate only
```

`task do` and CI regenerate and validate docs, so edit the schema/examples rather
than the generated Markdown.

## Releasing & Publishing

Releases are automated:

1. [release-please](https://github.com/googleapis/release-please) opens a release
   PR from Conventional Commits. Merging it tags `vX.Y.Z` and creates a GitHub
   release.
2. The `goreleaser` job in [release.yaml](.github/workflows/release.yaml) then
   builds the provider for all target platforms and attaches the artifacts the
   Terraform Registry ingests: the per-platform zips, `*_SHA256SUMS`,
   `*_SHA256SUMS.sig` (GPG detached signature), and `*_manifest.json`.

### One-time setup to publish to the Terraform Registry

1. Make this repository **public**.
2. Generate a GPG signing key (RSA/DSA, not ECC), then:
   - Add the **public** key at <https://registry.terraform.io> → *User Settings →
     Signing Keys*.
   - Add the **private** key and its passphrase as repository secrets
     `GPG_PRIVATE_KEY` (ASCII-armored) and `PASSPHRASE`.
3. On the registry, *Publish → Provider* and select this repo. The registry adds a
   release webhook and ingests each finalized release automatically.

Provider metadata for the registry lives in
[`terraform-registry-manifest.json`](terraform-registry-manifest.json)
(`protocol_versions: ["6.0"]`, terraform-plugin-framework).

Preview the rendered registry pages at
<https://registry.terraform.io/tools/doc-preview>.
