# T021 — Provider Interfaces & Wire Protocol [Completed]

* **Status:** Completed

## Goal
Establish the OpenAI-compatible wire protocol structures, streaming parser, and error normalization utilities in `internal/providers` supporting both LiteLLM and native OpenAI-compatible providers.

## Depends On
- T002 (Core Domain Types)
- T003 (Config Loader)

## Scope
* Define OpenAI wire request and response schemas:
  * Chat completion request (`model`, `messages`, `tools`, `stream`, `temperature`, `max_tokens`, `response_format`).
  * Message structures (`role`, `content`, `tool_calls`).
  * Tool definitions in OpenAI function schema format (`type: "function"`, `function: {name, description, parameters}`).
  * Chat completion response and streaming chunk schemas.
  * Embedding request and response schemas.
* Implement robust Server-Sent Events (SSE) stream scanner:
  * Read and trim `data: ` frames.
  * Detect `[DONE]` stream terminator.
  * Handle keep-alive / heartbeat comments (`: ping`).
  * Accumulate streaming tool call chunks (handling `index`, incremental `id`, `name`, and chunked `arguments`).
  * Parse usage stats from stream chunk or finish event (`usage` field with `prompt_tokens`, `completion_tokens`, `total_tokens`).
* Standardized error response parser:
  * Parse OpenAI-standard error envelopes (`{"error": {"message": ..., "type": ..., "code": ...}}`).
  * Map HTTP status codes (400, 401, 403, 404, 429, 500, 502, 503, 504) to Beavend error taxonomy (`core.ErrorResponse`).

## Acceptance Criteria
- [x] Complete serialization and deserialization between `core.ChatRequest`/`core.ChatResponse` and OpenAI wire format.
- [x] SSE stream scanner handles multiline data, partial reads, and heartbeat pings without buffer overflow.
- [x] Stream chunk tool call assembler reconstructs multi-argument tool calls accurately.
- [x] Upstream provider errors map correctly to Beavend `ErrorResponse` RFC 7807 taxonomy.
- [x] Comprehensive unit tests with race detection passing.

## Verification
```bash
CGO_ENABLED=0 go test -race -v ./internal/providers/...
```
