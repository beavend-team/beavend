# T007 — Type-Safe SQL Query Generation with Sqlc

## Goal
Configure `sqlc` to generate type-safe Go repository code from pure SQL queries in `internal/store`.

## Depends On
- T006

## Scope
* Configure `sqlc.yaml` targeting `jackc/pgx/v5`.
* Author SQL query files:
  * `projects.sql`: CRUD for projects and environments.
  * `ai_apps.sql`: Query active apps and app configurations.
  * `prompts.sql`: Query active prompt version by app ID.
  * `tools.sql`: Query tool definitions and active versions.
  * `policies.sql`: Query active CEL policies.
  * `snapshot.sql`: Single consolidated query or batch query fetching full active configuration for a project revision.

## Requirements
- Output package must be `internal/store/db`.
- Generate type-safe structs and queries using `pgx/v5` pool.

## Constraints
- No dynamic raw SQL string concatenation in application code.

## Acceptance Criteria
- [x] `sqlc generate` runs with 0 errors.
- [x] Integration tests verify inserting and querying project config using generated queries against a real PostgreSQL container.

## Verification
```bash
sqlc generate
go test -tags=integration -v ./internal/store/...
```
