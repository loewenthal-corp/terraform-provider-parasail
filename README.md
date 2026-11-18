# Terraform Provider for Parasail (Unofficial)

> **Unofficial / community project.** This provider is not built, maintained, or
> endorsed by Parasail. It is an independent project that talks to Parasail's
> **public** Control API only, and depends on no Parasail internal code. "Parasail"
> is used solely to describe the API the provider targets.

A Terraform and OpenTofu provider for [Parasail](https://www.parasail.io/)
dedicated model deployments, built on the `/api/v1/dedicated` Control API.

| Kind        | Name                                | Purpose                                                    |
| ----------- | ----------------------------------- | ---------------------------------------------------------- |
| Resource    | `parasail_dedicated_deployment`     | Create, update, import, and delete a dedicated deployment. |
| Data source | `parasail_dedicated_model_support`  | Check whether a model can be deployed, and on which engines. |
| Data source | `parasail_dedicated_device_configs` | List the GPU configurations, prices, and availability for a model. |
| Data source | `parasail_dedicated_deployments`    | List every deployment in the account.                      |

Full reference documentation is in [docs/](docs/) and on the Terraform Registry.

## Usage

```hcl
terraform {
  required_providers {
    parasail = {
      source  = "loewenthal-corp/parasail"
      version = ">= 0.2.0"
    }
  }
}

# Reads PARASAIL_API_KEY from the environment.
provider "parasail" {}

resource "parasail_dedicated_deployment" "qwen" {
  name  = "qwen-chat"
  model = "Qwen/Qwen2.5-7B-Instruct"

  # Repeat gpu blocks to offer alternatives; Parasail provisions each replica
  # on any listed configuration that has capacity.
  gpu {
    type  = "RTX5090"
    count = 1
  }

  gpu {
    type  = "H100SXM"
    count = 1
  }

  autoscaling {
    min_replicas = 1
    max_replicas = 4
  }

  scale_down_after = "2h" # or "never"
}

# OpenAI-compatible endpoint: POST {base_url}/chat/completions with model = model_alias.
output "endpoint" {
  value = {
    base_url = parasail_dedicated_deployment.qwen.base_url
    model    = parasail_dedicated_deployment.qwen.model_alias
  }
}
```

GPU types come from Parasail's catalog, which changes over time and differs per
model. Use `parasail_dedicated_device_configs` to pick one programmatically, as in
[examples/basic](examples/basic/main.tf), or check what's offered today:

```sh
curl -s -H "Authorization: Bearer $PARASAIL_API_KEY" \
  "https://api.parasail.io/api/v1/dedicated/devices?engine=VLLM&modelName=Qwen/Qwen2.5-7B-Instruct"
```

Existing deployments can be imported by their numeric ID, which
`parasail_dedicated_deployments` lists:

```sh
terraform import parasail_dedicated_deployment.qwen 12345
```

Provider settings can come from configuration or the environment:

| Argument                    | Environment variable | Default                          |
| --------------------------- | -------------------- | -------------------------------- |
| `api_key`                   | `PARASAIL_API_KEY`   | required                         |
| `endpoint`                  | `PARASAIL_ENDPOINT`  | `https://api.parasail.io/api/v1` |
| `request_timeout_seconds`   |                      | `60`                             |
| `poll_interval_seconds`     |                      | `30`                             |
| `operation_timeout_minutes` |                      | `60`                             |

More examples: [examples/fixed-gpu](examples/fixed-gpu/main.tf) (a fixed-size
deployment of a gated model) and the per-resource snippets under
[examples/resources](examples/resources/) and
[examples/data-sources](examples/data-sources/).

## Development

Tools (Go, Terraform, OpenTofu, golangci-lint, tflint, typos, gitleaks, trivy,
task) are pinned with [Hermit](https://cashapp.github.io/hermit/), so the only
prerequisite is a POSIX shell:

```sh
source bin/activate-hermit   # or put ./bin on PATH
task init                    # download Go modules and tflint plugins
task do                      # everything CI runs
```

`task --list` shows every target. The ones CI runs, each as its own job:

| Task                     | What it does                                                        |
| ------------------------ | ------------------------------------------------------------------- |
| `task format`            | `gofmt` and `tofu fmt`                                              |
| `task lint`              | golangci-lint, tflint, and typos                                    |
| `task examples:validate` | builds the provider and runs `tofu validate` on each `examples/*` directory |
| `task docs`              | regenerates `docs/` with tfplugindocs and validates it              |
| `task test`              | unit tests with `-race` and coverage                                |
| `task security`          | govulncheck, gitleaks, and trivy                                    |
| `task build`             | builds `dist/terraform-provider-parasail`                           |

CI also fails if any of these leave uncommitted changes, so commit regenerated
files.

### Code layout

- `internal/client`: a small HTTP client for the Parasail dedicated API.
- `internal/provider`: the provider, resource, and data sources
  (terraform-plugin-framework).
- `examples/`: runnable configurations and the snippets embedded in the docs.
- `templates/`: tfplugindocs templates that override the generated pages.
- `dev/provider-bugs.md`: provider bugs found during testing, and how they were
  fixed.

### Unit tests

`task test` needs no network or API key. Besides plain Go tests of the client,
schema, validators, and state mapping, `internal/provider/provider_unit_test.go`
drives the provider through the real Terraform CLI (from Hermit) against an
in-memory fake of the Parasail API in `fake_api_test.go`. Those cover create,
update, import, drift, out-of-band deletion, plan-time validation, and failed
waits. Extend the fake when the provider starts using a new endpoint or field.

### Acceptance tests

Acceptance tests run against the live Parasail API. Put a test key in `.env`
(it is gitignored) or export it:

```sh
echo 'PARASAIL_API_KEY=...' > .env
task test:acc:readonly   # data sources only; creates nothing
task test:acc            # full suite: creates, updates, imports, and deletes deployments
```

To keep a run cheap and fast, the mutating tests:

- pick the cheapest single-GPU configurations from the live
  `/dedicated/devices` catalog instead of hard-coding GPU types, so they keep
  working when Parasail changes its catalog;
- set `wait_for_online = false`, so they verify the API lifecycle without
  waiting for GPUs to be scheduled;
- name deployments `tfpacc-<test>-<unix time>` and, if a step fails, delete the
  test's own deployment by exact name in a cleanup hook. They never touch other
  deployments in the account.

After a run, `data "parasail_dedicated_deployments"` or the dashboard should show
no `tfpacc-` deployments.

In CI, the [acceptance workflow](.github/workflows/acceptance.yaml) runs on
weekdays, on pushes to `main`, and on pull requests labelled `live-acceptance`,
using the `PARASAIL_API_KEY` repository secret.

### Trying a local build

```sh
task build
cat > dev.tfrc <<EOF
provider_installation {
  dev_overrides {
    "registry.terraform.io/loewenthal-corp/parasail" = "$PWD/dist"
  }
  direct {}
}
EOF
cd examples/basic && TF_CLI_CONFIG_FILE=../../dev.tfrc terraform plan
```

## Releasing

Releases are automated from [Conventional Commits](https://www.conventionalcommits.org/):

1. Merge pull requests with conventional titles (`feat:`, `fix:`, `docs:`,
   `chore:`). The `conventional-pr` workflow checks PR titles.
2. [release-please](https://github.com/googleapis/release-please) keeps a release
   PR open with the next version and the `CHANGELOG.md` entries. `feat` bumps the
   minor version and `fix` bumps the patch version. Before 1.0, breaking changes
   also bump only the minor version.
3. Merging the release PR tags `vX.Y.Z` and creates a GitHub release.
4. The `goreleaser` job in [release.yaml](.github/workflows/release.yaml) builds
   the provider for every target platform and attaches the artifacts the
   Terraform Registry ingests: per-platform zips, `*_SHA256SUMS`,
   `*_SHA256SUMS.sig` (GPG detached signature), and `*_manifest.json`.
5. The registry's release webhook picks up the new version.

Don't edit `CHANGELOG.md` or `.release-please-manifest.json` by hand.

### One-time registry setup

1. Make this repository **public**.
2. Generate a GPG signing key (RSA or DSA, not ECC), then:
   - add the **public** key at <https://registry.terraform.io> → *User Settings →
     Signing Keys*;
   - add the **private** key and its passphrase as the repository secrets
     `GPG_PRIVATE_KEY` (ASCII-armored) and `PASSPHRASE`.
3. On the registry, choose *Publish → Provider* and select this repository.

Registry metadata lives in
[`terraform-registry-manifest.json`](terraform-registry-manifest.json)
(`protocol_versions: ["6.0"]`, terraform-plugin-framework). Preview rendered
pages at <https://registry.terraform.io/tools/doc-preview>.
