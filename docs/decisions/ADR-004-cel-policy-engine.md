# ADR-004 — CEL (Common Expression Language) for Authorization and Limit Tiers

## Status
Accepted

## Context
Beavend requires an expressive, high-performance rule engine to evaluate whether an authenticated end user is authorized to invoke an AI App or a Tool, and to determine which rate-limit tier applies based on JWT claims (e.g. `role`, `plan`, `ai_access`).

We considered:
1. Writing a custom expression parser and interpreter.
2. Embedding a scripting language like Lua or JavaScript (via Otto / Goja).
3. Using Open Policy Agent (Rego).
4. Using Google CEL (`google/cel-go`).

## Decision
We chose **Google CEL (Common Expression Language)** as the unified engine for authorization policies and rate-limit tier condition rules.

CEL programs are compiled once during snapshot creation and stored as compiled byte-code programs in the in-memory snapshot. Evaluation on incoming request claims executes in microseconds with zero memory allocations on the hot path.

## Consequences
### Positive
* **Safety:** CEL is sandboxed, memory-safe, non-Turing complete (no infinite loops or unbounded memory consumption), and cannot perform network or filesystem I/O.
* **Performance:** Pre-compiled CEL programs evaluate in single-digit microseconds.
* **Standardization:** CEL is a mature, well-documented standard with existing tooling and language parsers.
* **Validation:** Syntactic and type checking occur at configuration save-time in the control plane.

### Negative
* Contributors and administrators must learn basic CEL syntax (though simple boolean expressions match standard C-style/JavaScript syntax).

## Alternatives Considered
* **Custom Custom-Built Rule Parser:** Rejected due to ongoing maintenance burden, edge cases, and lack of type checking.
* **OPA / Rego:** Rejected as an internal engine due to heavier runtime overhead, though external OPA webhooks may be supported in v0.3.
* **Embedded Lua / JS:** Rejected because general-purpose scripting languages carry security risks, allow unbounded loops, and have higher memory overhead.
