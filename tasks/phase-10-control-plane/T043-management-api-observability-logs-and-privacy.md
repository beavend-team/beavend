# T043 - Management API: Observability, Logs & Privacy Operations

* **Status:** Completed

## Goal
Expose administrative endpoints to query aggregated usage analytics, search request metadata logs, and execute GDPR/privacy purge requests across data partitions.

## Depends On
- T041 (Admin Auth & RBAC)
- Phase 9 (Usage Tracking & Async Logging)

## Scope
1. **Usage Rollup Queries:**
   - `GET /v1/admin/usage`:
     - Query parameters: `project_id`, `environment`, `ai_app_id`, `user_id`, `granularity` (`hour` or `day`), `from` (ISO 8601), `to` (ISO 8601).
     - Returns timeseries aggregated metrics: request count, input tokens, output tokens, total tokens, cost USD, error count.
2. **Request Logs Inspection:**
   - `GET /v1/admin/logs`:
     - Query parameters: `project_id`, `environment`, `ai_app_id`, `user_id`, `status`, `from`, `to`, `limit`, `offset`.
     - Returns paginated operational request metadata (excluding raw message contents by default).
   - `GET /v1/admin/logs/{request_id}`:
     - Returns full metadata for a specific request ID including tool calls and error details.
3. **Privacy Purge (Subject Deletion):**
   - `DELETE /v1/admin/subjects/{issuer}/{subject}`:
     - GDPR / CCPA right-to-be-forgotten operation.
     - Owner role required.
     - Purges all request logs, usage events, and rollups for the specified subject identifier.
     - Purges any pending confirmation state or rate limit counters in Redis.
     - Records an immutable audit log entry documenting the purge execution.

## Acceptance Criteria
- [ ] Usage rollup endpoint returns accurate aggregated time series for hourly and daily buckets.
- [ ] Logs endpoint supports filtering, pagination, and sorting by timestamp descending.
- [ ] Privacy purge completely removes all records for the specified subject and records audit log.
- [ ] RBAC restricts privacy purge to `owner` role.
- [ ] Unit tests covering query builders, parameter validation, and privacy purge logic.
