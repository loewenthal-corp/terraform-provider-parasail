# Provider Bugs

This file tracks provider issues found during live/API validation. Use it for
bugs in provider behavior, Terraform schema shape, state handling, or API
mapping. Do not put Parasail platform/API incidents here unless the provider
needs to compensate for them.

## Open

No open provider bugs currently observed.

## Fixed

### PBUG-006: Invalid durations were only rejected during apply

- Status: fixed
- Found during: unit test review on 2026-11-18
- Impact: `scale_down_after = "1d"` or `max_request_duration = "0s"` passed
  `terraform validate` and `plan`, and only failed once apply built the API
  payload.
- Fix: added `scaleDownAfterValidator` and `positiveDurationValidator` to the
  schema, plus `AtLeast` validators on GPU counts, replica counts, and token
  limits, and a `SizeAtLeast(1)` validator on the `gpu` block.
- Verification: `TestDurationValidators`, `TestNumericAndBlockValidators`, and
  `TestUnitDedicatedDeployment_planTimeValidation`, which plans through the
  Terraform CLI.

### PBUG-005: Read and import dropped most attributes

- Status: fixed
- Found during: acceptance test review on 2026-11-18
- Impact: `Read` only refreshed `id`, `name`, `model`, `scale_down_after`, and
  the computed status fields. Out-of-band changes to GPUs, autoscaling,
  description, tags, engine, mode, or limits never showed up as drift, and
  `terraform import` left all of them empty, so the acceptance test had to
  ignore 18 attributes in `ImportStateVerifyIgnore`.
- Fix: `flattenDeployment` now maps every attribute Parasail returns, including
  only the device configs marked `selected`. It keeps the configured spelling
  when Parasail's value means the same thing (`480m` vs `8h`, GPU order, letter
  case, `AUTO` mode), so the extra mapping does not cause spurious diffs.
  `engine`, `mode`, and `scale_down_after` became optional+computed with schema
  defaults, and attributes Parasail fills in when omitted (token limits,
  autoscaling settings) became optional+computed. Import rejects non-numeric IDs.
- Verification: the live import steps now ignore only `wait_for_online`, which
  Parasail does not store. `TestFlattenDeployment*` and
  `TestUnitDedicatedDeployment_driftAndRemoval` cover the mapping and drift.

### PBUG-004: A failed wait for ONLINE leaked the deployment

- Status: fixed
- Found during: code review on 2026-11-18
- Impact: with `wait_for_online = true` (the default), `Create` only saved state
  after the deployment became `ONLINE`. If it entered `ERROR` or the wait timed
  out, Terraform recorded nothing, leaving a billable deployment that a later
  apply would try to create again under the same name.
- Fix: `Create` and `Update` save state as soon as Parasail accepts the request,
  then wait. A failed wait now leaves a tainted resource that Terraform replaces
  or destroys.
- Verification: `TestUnitDedicatedDeployment_failedWaitIsTracked` makes the fake
  API report `ERROR` and checks that the test's destroy removes the deployment.

### PBUG-003: Omitting `scale_down_after` broke apply

- Status: fixed
- Found during: code review on 2026-11-18
- Impact: `scale_down_after` was optional without being computed, but `Read`
  always filled it in from the API. Leaving it out (or writing an equivalent
  value such as `480m`) produced `Provider produced inconsistent result after
  apply`. Every example set it, which hid the bug.
- Fix: `scale_down_after` now defaults to `8h` in the schema, and the configured
  spelling is kept when Parasail returns the same duration.
- Verification: the live `TestAccDedicatedDeployment_autoscaling` minimal step
  and the fake-API unit tests apply configs without `scale_down_after`.

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
