# T047: Prompt Injection Defense & Size Limits

## Status
Completed

## Context & Objectives
Hostile user inputs may attempt prompt injection (jailbreaks, delimiter escaping, system instruction overrides) or Denial of Service via oversized payloads designed to exhaust memory or provider context windows.

Key requirements:
1. `SizeGuardrail`:
   - Enforces configurable byte boundaries on total input message payload (`max_input_bytes`, default 65536) and model output (`max_output_bytes`, default 65536).
   - Rejects oversized requests before reaching model providers to eliminate unnecessary token cost and memory pressure.
2. `InjectionGuardrail`:
   - Multi-pattern heuristic scanner for prompt injection and jailbreak signatures:
     - Direct system prompt override attempts (e.g. `ignore previous instructions`, `disregard all previous prompts`, `you are now in developer mode`).
     - Delimiter hijacking and synthetic role tags (e.g. `---BEGIN SYSTEM INSTRUCTIONS---`, `<|im_start|>system`, `[SYSTEM PROMPT]`).
     - Role-reversal exploits (e.g. `DAN mode enabled`, `jailbreak mode: true`, `do anything now`).
   - Normalizes input whitespace and case during evaluation to resist basic evasion.
   - Configurable sensitivity / rules.
3. Errors returned must carry structured details identifying the offending guardrail rule and reason.

## Deliverables
* `internal/guardrails/size.go`: Input and output byte length guardrail.
* `internal/guardrails/injection.go`: Prompt injection heuristic detector.
* `internal/guardrails/injection_test.go`: Unit tests with benchmark suites for injection detection patterns and size bounds.
