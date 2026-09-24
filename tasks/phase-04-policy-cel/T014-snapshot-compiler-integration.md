# T014 — Snapshot Compiler CEL Integration [Completed]

* **Status:** Completed

## Goal
Integrate the unified `policy.Compiler` into `internal/snapshot/compiler.go` so all named policies, inline AI App access policies, and limit tier conditions are compiled and validated during snapshot creation.

## Depends On
- T008
- T013

## Scope
* Refactor `internal/snapshot/compiler.go` to use `policy.Compiler`.
* Pre-compile:
  * Named policies in `Snapshot.Policies` into `Snapshot.CompiledPolicies`.
  * Inline AI App `AccessPolicy` expressions into `Snapshot.CompiledPolicies`.
  * Limit rule `TierCondition` expressions into `Snapshot.CompiledPolicies`.
* Validate referential integrity:
  * AI App `AccessPolicy` referencing an existing named policy or valid inline CEL expression.
  * Limit rules referencing valid CEL tier condition expressions.
* Ensure non-boolean policy expressions prevent snapshot compilation.

## Acceptance Criteria
- [x] Snapshot compiler leverages `policy.Compiler` without duplicated CEL environment setup.
- [x] Named and inline policies are accessible in `Snapshot.CompiledPolicies`.
- [x] Limit rule tier conditions are pre-compiled in `Snapshot.CompiledPolicies`.
- [x] Invalid CEL expressions fail snapshot compilation with descriptive errors.

## Verification
```bash
go test -race -v ./internal/snapshot/...
```
