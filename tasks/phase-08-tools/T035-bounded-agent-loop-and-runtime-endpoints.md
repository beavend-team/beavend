# T035: Bounded Agent Loop & Runtime Confirmation Endpoints

## Status
Completed

## Context & Objectives
Orchestrate the multi-turn agent execution loop and wire the HTTP confirmation / tool results endpoints:

### 1. Bounded Agent Loop (`internal/runtime/agent/loop.go`):
* Maximum depth N (default 5 iterations, configurable).
* In each iteration:
  1. Call model with current messages and active tools (from `AIApp.ToolIDs` in `Snapshot`).
  2. If model returns a final text message without tool calls → break and finish.
  3. If model returns one or more `ToolCall`s:
     - For each tool call:
       - Verify tool exists in snapshot.
       - Evaluate tool's CEL authorization policy (if configured) against caller `Identity`.
       - Inject identity bindings (discarding any model-supplied values for bound keys).
       - Validate arguments against JSON Schema.
       - If tool requires confirmation (`confirmation == "required"` or write tool without approval):
         - Freeze loop state in Redis.
         - Generate HMAC confirmation token.
         - Emit SSE `confirmation` event (or return confirmation response for unary).
         - Stop loop.
       - If tool is `client_side`:
         - Freeze loop state in Redis.
         - Emit SSE `tool_call` event with continuation token.
         - Stop loop.
       - If tool is server-side (`http`, etc.):
         - Execute via `core.Executor`.
         - Sanitize result (cap, redact, wrap as untrusted data).
         - Append tool result message (`role: "tool"`, `tool_call_id: call.ID`, `content: sanitized`).
  4. Continue loop to next iteration with appended tool messages.
  5. If max depth is exceeded, return bounded loop error.

### 2. Runtime Endpoints:
* `POST /v1/apps/{ai_app_id}/confirm`:
  - Validates `confirmation_token` and caller JWT.
  - If rejected (`approved: false`), appends cancellation message and resumes loop or finishes.
  - If approved (`approved: true`), executes the suspended tool, appends the result, and resumes the loop to produce the final answer.
* `POST /v1/apps/{ai_app_id}/tool-results`:
  - Validates `continuation_token` and caller JWT.
  - Injects client-provided tool result (sanitized as untrusted data) and resumes the loop.

## Deliverables
* `internal/runtime/agent/loop.go`: Bounded agent loop implementation.
* Integration into `internal/runtime/stages/execution.go`.
* Update `internal/runtime/handler.go` with `/confirm` and `/tool-results` handlers.
* Update `cmd/beavend/server.go` route wiring.
* End-to-end unit and integration tests.
