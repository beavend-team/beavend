# T083 - Rule-Based Resolver & RouteSelect Stage

* **Status:** Completed

## Goal
Replace hardcoded `vision`/`reasoning`/`fast` logic with rule evaluation and run selection before the cache.

## Scope
1. `internal/routing/resolver.go`: new `SelectLogicalModel` evaluating request `model` (policy-gated) → rules (first match) → default. Remove hardcoded names and `hasImageAttachment`. Error if nothing matches.
2. New `RouteSelectStage` placed before `CacheStage`; stores the selected logical model in `ReqState`.
3. `ModelRoutingStage` resolves deployment/adapter from the selected model; fallback resolution filtered by capability.
4. Table-driven tests: each modality, rule order, metadata, CEL, explicit override, no match.

## Acceptance Criteria
- [x] No reserved model names remain in code.
- [x] Existing routing tests updated and passing.
