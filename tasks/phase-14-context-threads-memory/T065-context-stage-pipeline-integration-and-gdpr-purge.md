# T065 — ContextStage Pipeline Integration, GDPR Subject Purge Hook & ADR-012

## Status
Completed

## Objective
Integrate context hydration into the runtime pipeline, execute GDPR subject privacy purging, and document architectural decisions in ADR-012.

## Key Changes
1. `ContextStage` (`internal/runtime/stages/context.go`):
   - Hydrates thread history and injects user-scoped semantic memories into request turns.
   - Preserves subject isolation by checking thread ownership.
2. Pipeline Integration:
   - `NewContextAwarePipeline` in `internal/runtime/pipeline.go`.
   - Wired in `cmd/beavend/main.go` and `cmd/beavend/server.go`.
3. GDPR Privacy Purge Hook:
   - `HandlePrivacyPurgeSubject` (`DELETE /v1/admin/subjects/{issuer}/{subject}`) invokes `Service.PurgeSubjectMemory`, deleting user-scoped memories and threads across memory providers and thread stores.
4. ADR-012:
   - Documented in `docs/decisions/ADR-012-conversation-threads-and-vector-memory-engine.md`.
5. Testing & Verification:
   - End-to-end integration suite in `internal/runtime/memory_e2e_test.go`.
