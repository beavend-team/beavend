# T029 - Pipeline Orchestrator & Execution Stage [Completed]

* **Status:** Completed

## Goal
Implement the sequential stage execution orchestrator in `internal/runtime` and the `ExecutionStage` in `internal/runtime/stages` to invoke provider adapters for unary and streaming chat completions.

## Depends On
- T025 (Model Routing Stage)
- T027 (Prompt Pipeline Stage)

## Scope
* Pipeline Orchestrator in `internal/runtime/pipeline.go`:
  * Manage ordered slice of `core.Stage`.
  * Traverse stages sequentially, tracking OpenTelemetry spans and execution latency.
  * Context propagation: check `ctx.Err()` between and inside stages to immediately abort on client cancellation.
  * Standardized error handling: catch errors, map to Beavend error taxonomy (`core.ErrorResponse`), populate `state.Error`.
* Execution Stage in `internal/runtime/stages/execution.go`:
  * Read `state.ResolvedModel` and look up `core.ProviderAdapter` from `routing.Resolver`.
  * Support Unary completions:
    * Call `adapter.Chat(ctx, *state.ChatRequest)`.
    * Populate `state.ChatResponse` with `id`, `created_at`, `message`, `finish_reason`, `usage`.
    * Update `state.Usage`.
  * Support Streaming completions:
    * Call `adapter.Stream(ctx, *state.ChatRequest)`.
    * Initialize `state.StreamChan` and forward stream events from adapter channel to `state.StreamChan`.
  * Support provider error mapping to `502 provider_error` or `504 provider_timeout`.
* Unit tests for pipeline execution, abort on error, context cancellation, unary completion, and stream forwarding.

## Acceptance Criteria
- [x] Pipeline runs stages in strict sequential order.
- [x] Any stage returning an error immediately halts further pipeline execution.
- [x] Unary requests invoke `adapter.Chat` and populate `state.ChatResponse`.
- [x] Streaming requests invoke `adapter.Stream` and stream chunks to `state.StreamChan`.
- [x] All tests pass with race detector enabled.
