# T022 - LiteLLM Provider Adapter [Completed]

* **Status:** Completed

## Goal
Implement the default `LiteLLMAdapter` in `internal/providers/litellm` conforming to `core.ProviderAdapter`.

## Depends On
- T021 (Provider Wire Protocol)

## Scope
* Implement `LiteLLMAdapter`:
  * Connect to LiteLLM instance via OpenAI-compatible endpoints (`/v1/chat/completions`, `/v1/embeddings`, `/health`).
  * Support Bearer token authentication via configured `LiteLLMConfig.Key`.
  * Support unary chat completion via `Chat(ctx, req)`.
  * Support streaming completion via `Stream(ctx, req)` yielding `<-chan core.StreamEvent`.
  * Support embedding generation via `Embed(ctx, req)`.
  * Support liveness/readiness health check via `Health(ctx)`.
* Cost & Usage Extraction:
  * Extract input/output/total tokens from LiteLLM usage payload.
  * Extract cost from LiteLLM `cost` / `x-litellm-response-cost` headers or usage extensions.
* Resiliency & Context:
  * Honor client context cancellation to abort upstream HTTP calls immediately.
  * Use custom HTTP client with reasonable timeout and connection pool settings.
  * Ensure stream reader goroutine exits cleanly on context cancellation without goroutine leaks.

## Acceptance Criteria
- [x] `LiteLLMAdapter` implements `core.ProviderAdapter`.
- [x] Unary chat, streaming chat, and embeddings tested against mock LiteLLM HTTP servers.
- [x] Cancellation of streaming context terminates the HTTP request and releases goroutines.
- [x] Usage and cost metrics properly extracted into `core.UsageStats`.
- [x] Unit tests pass with `CGO_ENABLED=0 go test -race -v ./internal/providers/litellm/...`.

## Verification
```bash
CGO_ENABLED=0 go test -race -v ./internal/providers/litellm/...
```
