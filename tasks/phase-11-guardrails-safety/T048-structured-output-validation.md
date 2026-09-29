# T048: Structured Output Schema Validation

## Status
Completed

## Context & Objectives
In generative AI workflows, clients often require responses formatted strictly according to a JSON Schema (`AIApp.ResponseSchema`). Even when models are instructed to produce JSON, LLMs may emit invalid JSON, markdown code-fencing wrappers (` ```json ... ``` `), or missing required fields.

Key requirements:
1. `SchemaGuardrail`:
   - Strips common LLM markdown code blocks (` ```json ... ``` `) before validation to handle common model formatting habits cleanly.
   - Validates JSON output against the compiled `jsonschema.Schema`.
   - Returns informative errors describing missing fields, type mismatches, or schema violations.
2. Snapshot pre-compilation:
   - Extend `internal/snapshot/compiler.go` to pre-compile `app.ResponseSchema` into `snap.CompiledSchemas["app_response:"+app.ID]` during snapshot compilation, ensuring zero DB reads and zero runtime compilation latency on the request hot path.
3. Integration with `core.ReqState`:
   - Automatically sanitizes/extracts the clean JSON payload for subsequent stages or response delivery.

## Deliverables
* `internal/guardrails/schema.go`: JSON Schema validator for LLM completions with markdown stripper.
* `internal/guardrails/schema_test.go`: Unit tests validating compliant, non-compliant, markdown-wrapped, and malformed JSON against various JSON schemas.
* Update `internal/snapshot/compiler.go`: Pre-compile `app.ResponseSchema` in snapshot compilation.
