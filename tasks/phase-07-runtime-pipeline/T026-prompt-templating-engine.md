# T026 — Prompt Templating Engine [Completed]

* **Status:** Completed

## Goal
Implement a robust, secure, in-memory prompt templating engine in `internal/prompts` supporting variable interpolation from identity claims, request metadata, and application configuration without arbitrary code execution.

## Depends On
- T002 (Core Domain Types)

## Scope
* Prompt template engine in `internal/prompts`:
  * Parse variable placeholders with standard mustache/curly syntax: `{{variable_name}}` and dot-path navigation `{{user.email}}`, `{{claims.role}}`.
  * Discover referenced variable names from a template string (`ExtractVariables`).
  * Render template string given a nested or flat variable map (`Render`).
  * Sanitize rendered output, leaving unmatched optional variables intact or blank per option.
  * Build unified variable context from `core.Identity`, request metadata, and application properties.
* Strict safety constraints:
  * Sandboxed execution: no function calls, no OS calls, zero arbitrary code execution.
  * Allocation-conscious string builder interpolation for sub-millisecond hot-path evaluation.
* Comprehensive table-driven unit tests with race detection.

## Acceptance Criteria
- [x] `ExtractVariables` extracts unique variable names from templates accurately.
- [x] `Render` correctly interpolates flat keys and nested dot paths (e.g., `user.sub`, `claims.tier`).
- [x] Safe fallback for missing variables without panics or corrupt formatting.
- [x] Zero database or disk I/O on evaluation path.
- [x] 100% unit test pass rate with race detection.

## Verification
```bash
CGO_ENABLED=0 go test -race -v ./internal/prompts/...
```
