# T015 — Authorization Pipeline Stage [Completed]

* **Status:** Completed

## Goal
Implement the runtime Authorization stage (`internal/runtime/stages/authz.go`) enforcing AI App access policies and tool authorization based on CEL evaluation.

## Depends On
- T002
- T013
- T014

## Scope
* Implement `AuthZStage` satisfying `core.Stage` (`Handle(ctx context.Context, state *core.ReqState) error`).
* Evaluate `state.AIApp.AccessPolicy` against `state.Identity` and `state.Metadata` using `policy.Evaluator`:
  * If policy is empty/unconfigured, access is permitted by default.
  * If policy evaluates to `true`, allow request to proceed to the next stage.
  * If policy evaluates to `false`, populate `state.Error` with `403 forbidden` and return error.
  * If policy program is missing or evaluation errors, deny with `403 forbidden`.
* Implement Tool authorization helper (`AuthorizeTool(ctx, snap, tool, id, meta)`):
  * Check tool access policy when model requests a tool call (per PRD §18.1).
* Telemetry & OpenTelemetry integration:
  * Emit span `stage.authz` with attributes: `policy_id`, `allowed`, `user_id`.
  * Record policy denial metrics.
* Comprehensive unit tests for `AuthZStage` and tool authorization with mocked request states.

## Acceptance Criteria
- [x] Requests matching policy succeed without error.
- [x] Requests failing policy set `state.Error` to 403 `forbidden` and halt pipeline.
- [x] Tool authorization correctly verifies tool-level policies.
- [x] OpenTelemetry spans and structured logs capture policy evaluation decisions.

## Verification
```bash
go test -race -v ./internal/runtime/stages/...
```
