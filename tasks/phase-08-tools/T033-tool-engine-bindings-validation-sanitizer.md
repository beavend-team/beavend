# T033: Identity Bindings, Schema Validation & Untrusted Result Sanitizer

## Status
Completed

## Context & Objectives
Ensure secure data handling between the LLM and downstream tools:

### 1. Identity Bindings (Confused-Deputy Defense)
* PRD §17.4 & Security §4.1: When tools declare `identity_bindings` (e.g. `user_id: identity.subject`), the values MUST be injected directly from the verified caller `Identity`.
* Any parameter value supplied by the LLM for identity-bound fields must be strictly discarded and overwritten with the server-verified value.
* Support extraction from `identity.subject`, `identity.issuer`, and arbitrary claims in `identity.claims[key]`.

### 2. JSON Schema Validation
* Validate model-provided arguments against the tool's configured `InputSchema` using `santhosh-tekuri/jsonschema/v5`.
* Pre-compiled schemas from the configuration snapshot should be reused for optimal performance.

### 3. Result Sanitization & Untrusted Data Wrapping
* Enforce `max_bytes` truncation (default 20KB).
* Redact sensitive patterns via regex (e.g. credit card numbers `(?:\d[ -]*?){13,16}`, SSNs `\b\d{3}-\d{2}-\d{4}\b`, and custom configured regexes).
* Wrap result in standard untrusted XML/markdown delimiters (e.g. `<tool_result id="..." name="..." status="success">...</tool_result>`) so the LLM cannot be hijacked by indirect prompt injection in external data.

## Deliverables
* `internal/tools/bindings.go`: Identity binding injection logic.
* `internal/tools/validator.go`: JSON Schema argument validation.
* `internal/tools/sanitizer.go`: Size capping, regex redaction, and untrusted delimiter wrapping.
* Unit tests covering confused-deputy override, schema violations, truncation, and regex redaction.
