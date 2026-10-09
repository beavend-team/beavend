# T036 - PostgreSQL Partitioned Tables for Request Logs & Usage Events

* **Status:** Completed

## Goal
Design and apply PostgreSQL schema migrations via Goose and generate type-safe sqlc queries for operational data: partitioned `request_logs`, partitioned `usage_events`, and aggregated `usage_rollups`.

## Depends On
- Phase 2 (Persistent Storage)

## Scope
1. **Goose Migration (`00003_usage_and_logs.sql`):**
   - Partitioned table `request_logs` (PARTITION BY RANGE (created_at)):
     - Columns: `id` (VARCHAR(64)), `request_id` (VARCHAR(64)), `project_id` (VARCHAR(64)), `environment` (VARCHAR(64)), `ai_app_id` (VARCHAR(64)), `user_id` (VARCHAR(255)), `user_subject` (VARCHAR(255)), `user_issuer` (VARCHAR(255)), `logical_model` (VARCHAR(128)), `provider` (VARCHAR(64)), `model_deployment` (VARCHAR(255)), `status` (VARCHAR(32)), `status_code` (INT), `latency_ms` (BIGINT), `input_tokens` (INT), `output_tokens` (INT), `total_tokens` (INT), `cost_usd` (NUMERIC(12,6)), `is_streaming` (BOOLEAN), `tool_calls` (JSONB), `error_code` (VARCHAR(64)), `error_message` (TEXT), `metadata` (JSONB), `created_at` (TIMESTAMPTZ).
     - Default partition and initial weekly/monthly partition slices.
   - Partitioned table `usage_events` (PARTITION BY RANGE (created_at)):
     - Columns: `id` (BIGSERIAL / UUID), `project_id`, `environment`, `ai_app_id`, `user_id`, `logical_model`, `provider`, `input_tokens`, `output_tokens`, `total_tokens`, `cost_usd`, `created_at`.
     - Default partition and initial partition slices.
   - Summary table `usage_rollups`:
     - Columns: `project_id`, `environment`, `ai_app_id`, `user_id`, `logical_model`, `provider`, `granularity` ('hour', 'day'), `bucket_start` (TIMESTAMPTZ), `request_count` (BIGINT), `input_tokens` (BIGINT), `output_tokens` (BIGINT), `total_tokens` (BIGINT), `cost_usd` (NUMERIC(14,6)), `error_count` (BIGINT), `updated_at` (TIMESTAMPTZ).
     - Primary key: `(project_id, environment, ai_app_id, user_id, logical_model, provider, granularity, bucket_start)`.
2. **sqlc Queries (`internal/store/queries/usage.sql`):**
   - Batch insert queries for `request_logs`.
   - Batch insert queries for `usage_events`.
   - Upsert query for `usage_rollups` (`ON CONFLICT (...) DO UPDATE`).
   - Query methods for querying logs and rollups with filters and pagination.
3. Generate Go code with `sqlc generate`.

## Acceptance Criteria
- [ ] Goose migration passes cleanly up and down.
- [ ] sqlc generates type-safe Go structs in `internal/store/db`.
- [ ] Partitioning allows transparent inserts into parent tables.
