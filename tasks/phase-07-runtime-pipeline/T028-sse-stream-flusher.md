# T028 - SSE Stream Flusher & Heartbeat Engine [Completed]

* **Status:** Completed

## Goal
Implement a high-performance Server-Sent Events (SSE) writer and flusher in `internal/runtime/stream` that streams event frames according to OpenAPI/PRD specifications, manages keep-alive heartbeat comments, and handles client disconnects gracefully.

## Depends On
- T002 (Core Domain Types)

## Scope
* `StreamWriter` in `internal/runtime/stream`:
  * Initialize HTTP response with required headers:
    * `Content-Type: text/event-stream; charset=utf-8`
    * `Cache-Control: no-cache, no-transform`
    * `Connection: keep-alive`
    * `X-Accel-Buffering: no`
  * Format event frames: `event: <name>\ndata: <json>\n\n`.
  * Support Beavend SSE event taxonomy:
    * `delta`: `{"text": "..."}`
    * `tool_call`: `{"call_id": "...", "tool": "...", "args": {...}}`
    * `confirmation`: `{"tool": "...", "args": {...}, "token": "..."}`
    * `usage`: `{"input_tokens": N, "output_tokens": N, "total_tokens": N}`
    * `error`: `{"code": "...", "message": "..."}`
    * `done`: `{"request_id": "..."}`
  * Heartbeat / keep-alive pings:
    * Emit `: ping\n\n` periodically (configurable, e.g. 15s) when stream is idle.
  * Flusher support:
    * Ensure immediate flushing via `http.Flusher`.
  * Disconnect detection:
    * Listen on `r.Context().Done()` to terminate streaming loops immediately and avoid goroutine leaks.
* Unit tests using `httptest.ResponseRecorder` and flusher mocks.

## Acceptance Criteria
- [x] Events formatted strictly with `event: ...\ndata: ...\n\n`.
- [x] Heartbeat comments periodically emitted during pauses.
- [x] Flushes after every single event chunk.
- [x] Closes cleanly on `done` event or client context cancellation.
- [x] 100% unit tests pass with race detection.
