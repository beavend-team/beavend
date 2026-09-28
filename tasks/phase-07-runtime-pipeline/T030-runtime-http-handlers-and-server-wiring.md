# T030 — Runtime HTTP Handlers & Server Wiring [Completed]

* **Status:** Completed

## Goal
Implement the runtime HTTP handlers in `internal/runtime` for `POST /v1/apps/{ai_app_id}/chat` and `POST /v1/apps/{ai_app_id}/stream`, integrate with identity auth middleware and snapshot resolution, wire into `cmd/beavend`, and verify end-to-end unary and streaming completion functionality.

## Depends On
- T028 (SSE Stream Flusher)
- T029 (Pipeline Orchestrator & Execution Stage)

## Scope
* `RuntimeHandler` in `internal/runtime/handler.go`:
  * `HandleChat(w http.ResponseWriter, r *http.Request)`:
    * Parse `X-Project-ID`, `X-Environment`, `X-Request-Id`, and URL path parameter `ai_app_id`.
    * Validate presence of project and app in snapshot registry.
    * Parse `core.ChatRequest` body with payload validation.
    * Build `core.ReqState` with `IsStreaming: false`.
    * Execute runtime pipeline.
    * Return JSON `core.ChatResponse` (HTTP 200) or error response (400, 401, 403, 404, 429, 502).
  * `HandleStream(w http.ResponseWriter, r *http.Request)`:
    * Same ingress validation as chat.
    * Build `core.ReqState` with `IsStreaming: true` and active channel.
    * Execute runtime pipeline.
    * Stream events using `StreamWriter`.
* Server wiring in `cmd/beavend/server.go` and `cmd/beavend/main.go`:
  * Initialize LiteLLM adapter and native OpenAI-compatible adapter.
  * Register adapters with `routing.Resolver`.
  * Instantiate all pipeline stages (`AuthZStage`, `LimitsStage`, `PromptStage`, `ModelRoutingStage`, `ExecutionStage`).
  * Construct pipeline and runtime handler.
  * Mount `/v1/apps/{ai_app_id}/chat` and `/v1/apps/{ai_app_id}/stream` with `identity.AuthMiddleware`.
* Full HTTP integration tests testing both unary chat and SSE streaming.

## Acceptance Criteria
- [x] End-to-end `POST /v1/apps/{id}/chat` returns 200 OK with valid JSON completion.
- [x] End-to-end `POST /v1/apps/{id}/stream` returns 200 OK with SSE chunks and terminal `done` event.
- [x] Missing or invalid auth token returns 401 unauthenticated.
- [x] Policy denial returns 403 forbidden.
- [x] Rate limit exhaustion returns 429 rate_limited with `retry_after`.
- [x] Unknown app returns 404 not_found.
- [x] Server builds and passes all tests with race detector.
