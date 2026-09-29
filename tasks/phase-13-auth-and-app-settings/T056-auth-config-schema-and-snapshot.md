# T056 — AuthConfig Database Schema, SQLC Queries & Snapshot Compilation

* **Status:** Completed

## Goal
Persist Identity Provider configuration (`core.AuthConfig`) in the `environments` table in PostgreSQL, generate sqlc queries, and load `AuthConfig` into the compiled in-memory `Snapshot` so the runtime can authenticate users without database queries on the request hot path.

## Scope
1. **Migration `00004_auth_config.sql`:**
   - Add column `auth_config JSONB NOT NULL DEFAULT '{}'::jsonb` to `environments` table.
2. **SQLC Queries (`internal/store/queries/projects.sql`):**
   - Update `ListEnvironments` and `CreateEnvironment` to select/insert `auth_config`.
   - Add `GetEnvironmentAuthConfig(project_id, name)` and `UpdateEnvironmentAuthConfig(project_id, name, auth_config)`.
   - Run `sqlc generate`.
3. **Snapshot Compilation (`internal/snapshot/compiler.go`):**
   - Query source retrieves the environment's `auth_config`.
   - Unmarshal JSON into `core.AuthConfig`.
   - Apply `identity.ResolveAuthConfig`.
   - Set `snap.AuthConfig = authCfg`.
4. **Unit Tests:**
   - Test snapshot compiler populating `snap.AuthConfig`.

## Acceptance Criteria
- [x] Database migration applies cleanly.
- [x] Sqlc generates type-safe Go structs and queries.
- [x] Snapshot compiler compiles `snap.AuthConfig` from the environment record.
- [x] Unit tests pass with `go test ./internal/snapshot/...`.
