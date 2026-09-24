# T013 — CEL Policy Compiler and Evaluator [Completed]

* **Status:** Completed

## Goal
Implement a Google CEL (Common Expression Language) compilation and evaluation engine in `internal/policy` for evaluating access policies and limit tier conditions against caller identity claims and request metadata.

## Depends On
- T002
- T011

## Scope
* Implement `policy.Compiler` using `google/cel-go`:
  * Define standard CEL environment with variables:
    * `claims` (`map[string]dyn`)
    * `identity` (`map[string]dyn`)
    * `meta` (`map[string]dyn`)
  * Type-check expressions to ensure they resolve to `cel.BoolType`.
  * Return compiled `cel.Program` instances.
* Implement `policy.Evaluator`:
  * Implement `core.PolicyEvaluator` interface (`Evaluate(ctx, policyID, id, meta)`).
  * Direct evaluation of `cel.Program` with `claims`, `identity`, and `meta` activation.
  * Evaluate expressions against `core.Snapshot.CompiledPolicies`.
  * Sub-microsecond evaluation on the hot path with zero DB calls.
* Comprehensive table-driven unit tests for all PRD §10 operators:
  * `eq`, `neq`, `in`, `not_in`, `gte`, `lte`, `regex` (`matches`), boolean combinations (`&&`, `||`, `!`).
  * Edge cases: nil identity, empty claims, missing fields.

## Acceptance Criteria
- [x] CEL environment correctly exposes `claims`, `identity`, and `meta`.
- [x] Non-boolean expressions fail compilation with typed errors.
- [x] Table-driven tests pass for all comparison, logical, membership, and regex expressions.
- [x] Microsecond-level evaluation performance without memory leaks.

## Verification
```bash
go test -race -v ./internal/policy/...
```
