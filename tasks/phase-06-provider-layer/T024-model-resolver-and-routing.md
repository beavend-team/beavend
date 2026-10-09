# T024 - Model Resolver & Routing [Completed]

* **Status:** Completed

## Goal
Implement logical model resolution, adapter dispatch, and fallback deployment management in `internal/routing`.

## Depends On
- T002 (Core Domain Types)
- T022 (LiteLLM Adapter)
- T023 (OpenAI-Compat Adapter)

## Scope
* Implement `ModelResolver`:
  * Map logical model name (or request override) to concrete `*core.ModelRouting` using in-memory `*core.Snapshot`.
  * Support rule-based routing or direct logical model alias lookup (`fast`, `reasoning`, `vision`).
  * Resolve concrete provider deployment and adapter (`litellm` vs `openai_compat`).
  * Support fallback deployments list if primary deployment is marked down or experiences transient errors.
* Provider Adapter Registry:
  * Register adapters by type (`litellm`, `openai_compat`).
  * Lookup and return the appropriate `core.ProviderAdapter` along with resolved deployment string.
* Invariant Check:
  * Zero PostgreSQL reads on the hot path (all model routings are resolved directly from `*core.Snapshot`).

## Acceptance Criteria
- [x] Resolves logical models directly from snapshot without DB queries.
- [x] Correctly resolves primary deployment and fallback deployment chains.
- [x] Returns correct adapter instance (`LiteLLMAdapter` vs `OpenAICompatAdapter`).
- [x] Table-driven unit tests pass with `CGO_ENABLED=0 go test -race -v ./internal/routing/...`.

## Verification
```bash
CGO_ENABLED=0 go test -race -v ./internal/routing/...
```
