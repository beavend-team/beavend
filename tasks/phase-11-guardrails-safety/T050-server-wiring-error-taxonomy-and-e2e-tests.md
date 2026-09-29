# T050: Server Wiring, Error Taxonomy & End-to-End Validation

## Status
Completed

## Context & Objectives
Wire the guardrail stages into the server runtime pipeline, map guardrail violation errors cleanly to HTTP status 422 (`guardrail_blocked`), record guardrail violation events in usage metrics and request logs, and validate the end-to-end flow with unit and integration tests.

Key requirements:
1. Update `internal/runtime/pipeline.go` (`NewStandardPipeline`) and `cmd/beavend/main.go` to construct and configure the guardrail stages.
2. Error taxonomy: Ensure `core.ErrorResponse` with code `guardrail_blocked` maps to HTTP `422 Unprocessable Entity` in HTTP handlers.
3. Telemetry & Metrics:
   - Record `guardrail_violations_total` counter in Prometheus metrics labeled with `project_id`, `app_id`, and `guardrail_type`.
   - Propagate guardrail violation details into `reqState.Usage` / `reqState.Metadata`.
4. End-to-end integration tests:
   - Test unary chat blocked by prompt injection (HTTP 422).
   - Test unary chat with PII redaction.
   - Test streaming chat in `buffered` mode blocking invalid structured JSON.
   - Test streaming chat in `passthrough_with_post_check` emitting post-check error.

## Deliverables
* Update `internal/runtime/pipeline.go` and `cmd/beavend/main.go`.
* Update HTTP error mapping in `internal/runtime/handler.go` for 422 `guardrail_blocked`.
* Add Prometheus metrics for guardrail violations in `internal/telemetry/metrics.go`.
* `internal/runtime/guardrails_e2e_test.go`: End-to-end test suite testing input rejection, output schema enforcement, PII redaction, and streaming modes.
