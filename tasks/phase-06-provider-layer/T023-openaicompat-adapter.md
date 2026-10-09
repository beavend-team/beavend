# T023 - Native OpenAI-Compatible Adapter [Completed]

* **Status:** Completed

## Goal
Implement the high-performance native Go `OpenAICompatAdapter` in `internal/providers/openaicompat` conforming to `core.ProviderAdapter` for direct model provider access (OpenAI, OpenRouter, vLLM, Ollama).

## Depends On
- T021 (Provider Wire Protocol)

## Scope
* Implement `OpenAICompatAdapter`:
  * Direct HTTP client calling OpenAI wire format endpoints.
  * Configurable base URL, API key, optional organization ID, and custom headers.
  * Chat completions (`/chat/completions`) unary and streaming.
  * Embeddings (`/embeddings`).
  * Health check via probe or `/models`.
* High-throughput tuning:
  * Persistent HTTP connection pooling (`MaxIdleConns`, `IdleConnTimeout`, `KeepAlive`).
  * Direct zero-allocation stream chunk processing.

## Acceptance Criteria
- [x] `OpenAICompatAdapter` implements `core.ProviderAdapter`.
- [x] Supports OpenAI, OpenRouter, and Ollama endpoint formats.
- [x] Streaming events delivered without excess buffering.
- [x] Unit tests pass with `CGO_ENABLED=0 go test -race -v ./internal/providers/openaicompat/...`.

## Verification
```bash
CGO_ENABLED=0 go test -race -v ./internal/providers/openaicompat/...
```
