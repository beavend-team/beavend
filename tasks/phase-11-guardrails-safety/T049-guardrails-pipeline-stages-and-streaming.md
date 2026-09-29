# T049: Guardrail Pipeline Stages & Streaming Execution Modes

## Status
Completed

## Context & Objectives
To enforce guardrails smoothly within Beavend's request pipeline, we must introduce two specialized stages:
1. `InputGuardrailStage`: Runs before prompt interpolation and model invocation. Checks user messages against input byte limits, prompt injection, and input PII.
2. `OutputGuardrailStage`: Runs after model completions or tool agent loop. Checks assistant text against output byte limits, output PII, and structured JSON Schema.

Streaming modes handling:
According to PRD §20.1, streaming models present a fundamental tradeoff:
- `stream_mode: "buffered"`:
  - Collects chunks in memory during streaming.
  - Executes output guardrail checks on the full generated completion.
  - If checks pass, flushes all chunks to the client and emits `done`.
  - If checks fail, discards the buffered text and emits an SSE `error` event with code `guardrail_blocked`.
- `stream_mode: "passthrough_with_post_check"`:
  - Emits chunks immediately as they arrive from the provider adapter.
  - Upon completion, executes output guardrails on the assembled text.
  - If checks fail, emits an SSE `error` event, increments error metrics, and marks the request log with the guardrail violation.

## Deliverables
* `internal/runtime/stages/guardrail_input.go`: Stage enforcing input size, injection defense, and input PII.
* `internal/runtime/stages/guardrail_output.go`: Stage enforcing output size, output PII, and structured schema.
* `internal/runtime/stages/guardrails_test.go`: Unit tests validating input rejection, PII masking, schema verification, and pipeline error halts.
* Stream handler support for `buffered` vs `passthrough_with_post_check` modes in `internal/runtime/handler.go` and `internal/runtime/stream/`.
