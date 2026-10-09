# T039 - Usage Rollup Background Aggregator

* **Status:** Completed

## Goal
Implement a periodic background aggregator that rolls up raw `usage_events` into hourly and daily summary records in `usage_rollups`.

## Depends On
- T036 (Usage Schema)
- T037 (Batch Writer)

## Scope
1. **Rollup Aggregator Service (`internal/usage/rollup.go`):**
   - Configurable aggregation schedule (e.g. runs every 1 to 5 minutes, or hourly).
   - Queries `usage_events` within the previous un-aggregated window (e.g. `bucket_start` to `bucket_end`).
   - Computes:
     - Total request count.
     - Total input tokens, output tokens, total tokens.
     - Total estimated USD cost.
     - Error counts.
   - Upserts into `usage_rollups` with granularity (`hour`, `day`).
   - Tracks checkpoint of last processed timestamp to avoid redundant scans.
2. **Postgres Rollup Queries:**
   - Efficient SQL aggregation query using `SUM()`, `COUNT()` grouped by `(project_id, environment, ai_app_id, user_id, logical_model, provider, date_trunc('hour', created_at))`.
3. Unit tests simulating raw event sets and verifying accurate hourly/daily rollup values and idempotency on re-run.

## Acceptance Criteria
- [ ] Rollups correctly group and sum tokens, costs, and request counts.
- [ ] Aggregator is idempotent (re-aggregating the same window does not duplicate stats).
- [ ] Handles empty windows gracefully without error.
