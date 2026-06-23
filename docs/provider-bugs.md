# Provider Bugs

This file tracks provider issues found during live/API validation. Use it for
bugs in provider behavior, Terraform schema shape, state handling, or API
mapping. Do not put Parasail platform/API incidents here unless the provider
needs to compensate for them.

## Open

No open provider bugs currently observed.

## Fixed

### PBUG-002: Deployment name constraints were only enforced by the API

- Status: fixed
- Found during: live acceptance test on 2026-06-23
- Impact: Terraform accepted deployment names longer than Parasail's API limit,
  then failed during create with `Deployment name must be between 2 and 32
  characters. Only lowercase letters, numbers, and hyphens.`
- Repro:

  ```hcl
  resource "parasail_dedicated_deployment" "too_long" {
    name  = "tf-provider-acc-autoscale-1234567890"
    model = "Qwen/Qwen2.5-7B-Instruct"

    gpu {
      type  = "L40S"
      count = 1
    }
  }
  ```

- Fix: added Terraform schema validators for the `name` attribute length and
  allowed character set.
- Verification: full live acceptance suite passed after the fix.

### PBUG-001: Fixed-size deployment incorrectly required `autoscaling.max_replicas`

- Status: fixed
- Found during: local live apply smoke test on 2026-06-23
- Impact: A `parasail_dedicated_deployment` without an `autoscaling` block failed
  during planning with `Missing Configuration for Required Attribute` for
  `autoscaling.max_replicas`, even though fixed-size deployments should be valid.
- Repro:

  ```hcl
  resource "parasail_dedicated_deployment" "smoke" {
    name  = "tf-provider-smoke"
    model = "Qwen/Qwen2.5-7B-Instruct"

    gpu {
      type  = "L40S"
      count = 1
    }
  }
  ```

- Fix: changed nested `autoscaling.max_replicas` from required to optional in
  `internal/provider/dedicated_deployment_resource.go`.
- Verification: the same smoke config planned, applied, appeared in
  `parasail_dedicated_deployments`, destroyed, and disappeared from the list.
