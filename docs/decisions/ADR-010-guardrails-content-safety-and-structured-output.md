# ADR-010: Guardrails, Content Safety, and Structured Output

## Status
Accepted

## Context
Applications deploying generative AI capabilities to end users require safeguards against prompt injection, data exfiltration, sensitive Personally Identifiable Information (PII) leakage, unbounded payload consumption, and schema non-conformity. Furthermore, Server-Sent Events (SSE) streaming poses a fundamental tradeoff between streaming latency and safety verification, as once a token has been written to the client TCP socket, it cannot be physically retracted.

In accordance with PRD §20, §22, and Architecture §5, Beavend requires a modular guardrail engine and pipeline stages to inspect inputs and outputs while preserving the core zero-DB-read hot-path invariant and standard error taxonomy.

## Decision
1. **Pipeline Placement & Modular Engine:**
   - **Input Guardrails Stage:** Positioned in the runtime request pipeline immediately after authorization/rate limiting and before prompt templating and model execution. Prevents hostile or oversized prompts from reaching LLMs and incurring provider cost.
   - **Output Guardrails Stage:** Positioned after model execution / agent loop and before client delivery. Validates output byte size, sanitizes PII, and verifies structured JSON Schema conformance.
   - Guardrail rules are defined in `internal/guardrails` and implement the `core.Guardrail` interface.
2. **Defensive Capabilities:**
   - **Payload Sizing:** Enforces `max_input_bytes` (default 64 KB) and `max_output_bytes` (default 64 KB). Violations reject the request immediately with `422 guardrail_blocked`.
   - **PII Detection & Redaction:** Regular expression and pattern matching against common sensitive data formats (credit cards using Luhn validation, Social Security Numbers, email addresses, phone numbers, and bearer/JWT tokens). Configurable to redact (masking tokens in-place) or block execution.
   - **Prompt Injection Defense:** Multi-pattern heuristic scanner targeting jailbreak signatures, system prompt override attempts, role-switching exploits, and delimiter manipulation.
   - **Structured Output Validation:** Pre-compiles JSON Schemas defined on `AIApp.ResponseSchema` into `Snapshot.CompiledSchemas` during snapshot compilation. Validates assistant responses against the schema before delivery.
3. **Streaming Semantics:**
   - `stream_mode: buffered`: Assembles stream deltas in memory; executes full output guardrail and schema validation before emitting deltas to the client. Guarantees 100% safety at the cost of time-to-first-token latency.
   - `stream_mode: passthrough_with_post_check`: Delivers SSE stream deltas immediately with zero buffering latency. Upon stream completion, executes output checks against the assembled text. If a violation occurs, logs the violation to usage metrics and emits an SSE `error` event with code `guardrail_blocked`.
4. **Zero-DB-Read Hot-Path Invariant:**
   - Guardrail settings (`GuardrailConfig`) and compiled JSON schemas are stored in the memory snapshot (`core.Snapshot`) and updated via atomic pointer swaps. No database queries are performed during guardrail evaluation.
5. **Standardized Error Taxonomy & Observability:**
   - Guardrail rejections return HTTP status `422 Unprocessable Entity` with error code `guardrail_blocked` and violation details in the RFC 7807 error payload.
   - Violations are recorded on `ReqState.Usage` and exported to Prometheus metrics and request audit telemetry.

## Consequences
### Positive
* Comprehensive defense-in-depth against prompt injection, PII leakage, and schema invalidity.
* Configurable streaming modes give implementors control over the latency vs safety tradeoff.
* Sub-millisecond execution overhead using compiled regular expressions and in-memory JSON schemas.
* Full compatibility with zero-DB-read architectural invariant.

### Negative
* `buffered` streaming mode delays time-to-first-token until completion or chunk validation.
* Pattern-based prompt injection detection cannot detect 100% of novel adversarial jailbreaks without auxiliary semantic classifier models (planned for future external policy plugins).

## Alternatives Considered
* **External Guardrail Microservice:** Rejected for the default path due to network latency penalties (50-200ms) on every user request. Pluggable external evaluators remain supported via interface extension.
* **Pure CEL-Based Guardrails:** Rejected because regular expressions, Luhn checksums, and JSON Schema trees are more expressive and performant in native Go than interpreted CEL ASTs for text stream scanning.
