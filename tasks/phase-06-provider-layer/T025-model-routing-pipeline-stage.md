# T025 - Model Routing Pipeline Stage [Completed]

* **Status:** Completed

## Goal
Implement the Model Routing Stage in `internal/runtime/stages` conforming to `core.Stage` to resolve model routing during request execution.

## Depends On
- T024 (Model Resolver)

## Scope
* Implement `ModelRoutingStage`:
  * Implements `core.Stage`.
  * Extracts Snapshot and AI App from `core.ReqState`.
  * Resolves logical model to deployment and provider adapter.
  * Injects `state.ResolvedModel` into request state.
  * Validates model availability, returning normalized error (`404 not_found` if model routing missing, `502 provider_error` if misconfigured).
* OpenTelemetry Tracing & Metrics:
  * Record span `stages.model_routing`.
  * Add attributes `model.logical`, `model.deployment`, `model.adapter`.

## Acceptance Criteria
- [x] Populates `state.ResolvedModel` with accurate deployment and adapter details.
- [x] Returns structured `core.ErrorResponse` on resolution failure.
- [x] OpenTelemetry span and attributes created.
- [x] Unit tests pass with `CGO_ENABLED=0 go test -race -v ./internal/runtime/stages/...`.

## Verification
```bash
CGO_ENABLED=0 go test -race -v ./internal/runtime/stages/...
```
