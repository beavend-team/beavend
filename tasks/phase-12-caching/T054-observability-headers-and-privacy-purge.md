# T054: Observability, Headers, Post-Execution Writing & Privacy Invalidation

## Status
Completed

## Context & Objectives
To provide complete transparency into caching behavior:
1. Ingress and egress handlers must communicate caching status via standard headers (`X-Cache: HIT`, `X-Cache: MISS`).
2. Post-execution response caching: when a cache miss occurs on an app with caching enabled, the final response must be written to the cache with the configured TTL upon successful completion.
3. Prometheus metrics must track cache performance:
   - `beavend_cache_requests_total`: counter partitioned by `cache_type` ("response", "tool") and `status` ("hit", "miss").
4. Privacy operations: The admin endpoint `DELETE /v1/admin/subjects/{issuer}/{subject}` must purge all response and tool cache keys associated with that subject.

## Deliverables
* Update `internal/runtime/handler.go` to set `X-Cache` headers and save completed responses on cache misses.
* Update `internal/control/handlers_observability.go` to invoke `CacheStore.DeleteSubjectKeys` during `HandlePrivacyPurgeSubject`.
* Prometheus cache metrics in `internal/telemetry` or runtime stages.
* Unit tests validating header propagation and privacy deletion.
