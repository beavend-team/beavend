# T017 - Tier Resolution Engine [Completed]

* **Status:** Completed

## Goal
Implement the tier resolution engine in `internal/limits` that maps caller identity claims to the applicable `core.LimitRule`s using CEL evaluation.

## Depends On
- T013 (CEL Policy Compiler and Evaluator)
- T016 (Redis Limits)

## Scope
* Implement `TierResolver`:
  * Evaluates `TierCondition` expressions on `core.LimitRule` using `policy.Compiler` / `policy.Evaluator`.
  * Pre-compiles CEL conditions or caches compiled programs for fast hot-path evaluation.
  * Resolves active rules for caller: matches specific tier rules, falling back to default rules when conditions do not match.
  * Deduplicates and aggregates active rules per resource and window.
* Comprehensive table-driven tests:
  * User with free tier claim (`plan == 'free'`) resolves to free limits.
  * User with pro tier claim (`plan == 'pro'`) resolves to pro limits.
  * User with no matching tier claim falls back to default limits.
  * Mixed resource rules (e.g. inference + tokens) resolve properly.

## Acceptance Criteria
- [x] Sub-microsecond tier resolution on hot path.
- [x] Accurate rule precedence (explicit tier rule overrides default rule for the same resource+window).
- [x] Table-driven unit tests pass.

## Verification
```bash
CGO_ENABLED=0 go test -race -v ./internal/limits/...
```
