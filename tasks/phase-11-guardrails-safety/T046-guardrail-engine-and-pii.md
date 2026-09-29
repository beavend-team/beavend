# T046: Guardrail Engine & PII Redaction/Detection

## Status
Completed

## Context & Objectives
In generative AI applications, user prompts and model completions frequently contain sensitive data such as credit card numbers, Social Security Numbers (SSNs), email addresses, phone numbers, and bearer authentication tokens / JWTs. Beavend must provide a fast, memory-safe PII detection and redaction engine in `internal/guardrails` satisfying the `core.Guardrail` interface.

Key requirements:
1. Provide a unified `Engine` in `internal/guardrails` coordinating multiple modular rule checks.
2. Implement `PIIGuardrail`:
   - Detects and validates credit card numbers using regex pattern matching combined with the Luhn algorithm to eliminate false positives.
   - Detects US Social Security Numbers (SSNs).
   - Detects email addresses.
   - Detects telephone numbers in standard formats (E.164, US local/international).
   - Detects bearer tokens / API keys / JWTs.
3. Support two operational modes for PII:
   - `Redact`: Replaces sensitive tokens with placeholders (e.g. `[REDACTED_CREDIT_CARD]`, `[REDACTED_EMAIL]`, `[REDACTED_SSN]`).
   - `Block`: Returns a guardrail violation error halting execution with code `guardrail_blocked`.
4. High performance: pre-compiled regular expressions, zero heap allocations where possible, sub-millisecond execution.

## Deliverables
* `internal/guardrails/engine.go`: Multi-rule coordinator implementing input/output evaluation.
* `internal/guardrails/pii.go`: Regex and Luhn-based PII detector and redactor.
* `internal/guardrails/pii_test.go`: Comprehensive unit tests covering credit cards (valid/invalid Luhn), SSNs, emails, phone numbers, JWTs, and redaction verification.
