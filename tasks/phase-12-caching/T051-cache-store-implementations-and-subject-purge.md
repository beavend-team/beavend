# T051: Cache Store Implementations & Subject Purge

## Status
Completed

## Context & Objectives
To reduce upstream model latency and token costs, Beavend requires high-performance ephemeral caching. In accordance with Architecture §7.3, cache stores must support Redis/Valkey for distributed multi-instance deployments and an in-memory TTL store for local development or Redis-less operation. Furthermore, to uphold the tenant and identity isolation invariants, all cache keys must be strictly scoped by project, environment, and user subject (unless explicitly marked shared).

Key requirements:
1. Implement `RedisCacheStore` in `internal/cache` using `github.com/redis/go-redis/v9`.
2. Implement `MemoryCacheStore` in `internal/cache` using safe concurrent maps and time-based expiration.
3. Provide key generation utilities satisfying the canonical key formats:
   - Response cache: `rc:{project}:{env}:{scope}:{hash}` (where scope is `shared` or `user:{subject}`)
   - Tool result cache: `tc:{project}:{env}:{user}:{tool}:{argshash}`
4. Support pattern-based deletion and subject purging for privacy compliance (`DELETE /v1/admin/subjects/{issuer}/{subject}`).
5. Comprehensive unit tests covering Get, Set, Delete, TTL expiration, and Subject deletion.

## Deliverables
* `internal/cache/cache.go`: `CacheStore` implementations (`RedisCacheStore`, `MemoryCacheStore`) and key builder helpers.
* `internal/cache/cache_test.go`: Table-driven tests validating caching operations, TTL behavior, and pattern purging.
