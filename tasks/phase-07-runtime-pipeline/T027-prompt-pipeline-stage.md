# T027 - Prompt Pipeline Stage [Completed]

* **Status:** Completed

## Goal
Implement the `PromptStage` in `internal/runtime/stages` that resolves the active prompt version from the configuration snapshot, interpolates dynamic variables from user identity and request metadata, and formats the message array with the system prompt.

## Depends On
- T002 (Core Domain Types)
- T026 (Prompt Templating Engine)

## Scope
* `PromptStage` struct implementing `core.Stage`:
  * Read `state.AIApp.PromptID` and `state.AIApp.PromptVersion` from the snapshot.
  * Extract active `PromptVersion` from `state.Snapshot.Prompt(promptID)`.
  * If no prompt configured on app, pass through without modification.
  * Populate variable context with `state.Identity` (subject, issuer, claims), project ID, environment, and `state.Metadata`.
  * Render template using the prompt engine.
  * Inject the rendered prompt as a system `ChatMessage` (`role: "system"`) at the head of `state.ChatRequest.Messages`.
  * Handle edge cases: existing system message merging/replacement, missing required prompt error handling.
* Tracing & Telemetry:
  * OpenTelemetry span `stage.prompt` with attributes (`prompt.id`, `prompt.version`, `prompt.char_count`).
* Unit tests covering missing prompt, variable substitution, and message ordering.

## Acceptance Criteria
- [x] Correctly resolves prompt from snapshot in-memory without database queries.
- [x] Injects rendered system prompt into `state.ChatRequest.Messages` at position 0.
- [x] Accurately injects identity claims (e.g. `user_id`, `email`, `role`) into prompt variables.
- [x] Returns structured error if app requires a non-existent prompt.
- [x] Unit tests pass with `CGO_ENABLED=0 go test -race -v ./internal/runtime/stages/...`.
