# T019 - PostgreSQL Fallback Limit Store [Completed]

* **Status:** Completed

## Goal
Implement a database-backed `LimitStore` using PostgreSQL for minimal, single-node deployments running without Redis.

## Depends On
- T006 (Postgres Migrations)
- T016 (Limit Store Interface)

## Scope
* Create Goose migration `00002_rate_limits.sql`:
  * Table `rate_limit_counters`:
    * `key VARCHAR(255) NOT NULL`
    * `resource VARCHAR(32) NOT NULL`
    * `window_seconds INT NOT NULL`
    * `bucket_start TIMESTAMPTZ NOT NULL`
    * `current_count BIGINT NOT NULL DEFAULT 0`
    * `expires_at TIMESTAMPTZ NOT NULL`
    * `PRIMARY KEY (key, resource, window_seconds, bucket_start)`
  * Index on `expires_at` for background cleanup.
* Implement `PostgresLimitStore`:
  * Implements `core.LimitStore`.
  * Atomic upsert with `INSERT ... ON CONFLICT DO UPDATE`.
  * Handles `CheckAndDebit` atomically across rules within a database transaction or atomic statement.
  * Handles `ReconcileTokens`.
* Unit / integration tests for Postgres counter operations.

## Acceptance Criteria
- [x] Goose migration adds `rate_limit_counters` table with proper indexes and down migration.
- [x] Atomic check-and-debit semantics identical to Redis implementation.
- [x] Zero race conditions under concurrent requests.
- [x] Unit tests pass.

## Verification
```bash
CGO_ENABLED=0 go test -race -v ./internal/limits/...
```
