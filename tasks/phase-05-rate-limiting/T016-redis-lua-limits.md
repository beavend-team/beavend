# T016 - Redis Lua Multi-Window Limit Store [Completed]

* **Status:** Completed

## Goal
Implement the primary atomic multi-window rate limiter in `internal/limits` using Redis/Valkey with an embedded Lua script.

## Depends On
- T002 (Core Domain Types)
- T003 (Config Loader)

## Scope
* Implement `RedisLimitStore` implementing `core.LimitStore`.
* Implement the multi-window sliding window Lua script:
  * Redis Cluster compatible keys using hash tags: `{lim:project:env:user_hash}:resource:window`.
  * Support multiple rules simultaneously (e.g. 5 req/1m AND 30 req/24h AND 100,000 tokens/24h).
  * Atomic check-and-debit in a single round-trip: if all rules pass, all counters increment; if any rule fails, zero counters increment.
  * Return whether allowed and the maximum `retry_after` duration across exceeded rules.
  * Automatic TTL expiration on counter keys.
* Unit and mock tests using in-memory or mini-redis / table-driven test cases for:
  * Multi-window bursts (passes within window, blocked upon exceeding max).
  * Different resources (`inference`, `tokens`).
  * Cluster hash-tag validation.
  * Fail-closed handling when Redis returns errors or is unavailable.

## Acceptance Criteria
- [x] Atomic check-and-debit in a single Redis `EVALSHA` / `EVAL` execution.
- [x] Accurate `retry_after` calculation.
- [x] All keys properly hash-tagged for Redis Cluster compatibility.
- [x] Fail-closed behavior on connection error if configured.
- [x] Unit tests pass with race detector enabled.

## Verification
```bash
CGO_ENABLED=0 go test -race -v ./internal/limits/...
```
