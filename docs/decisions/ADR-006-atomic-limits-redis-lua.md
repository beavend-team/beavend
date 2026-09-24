# ADR-006 — Atomic Multi-Window Limits in Redis with PostgreSQL Fallback

## Status
Accepted

## Context
AI backend requests incur significant financial cost and compute load. Beavend must enforce per-user rate limits and quotas across multiple sliding or fixed time windows simultaneously (e.g. 5 requests/minute AND 30 requests/day AND 100,000 tokens/day).

Checking and incrementing multiple counters across distributed server instances cannot suffer from race conditions or require multiple network round trips. Additionally, small hobbyist self-hosters should ideally have the option to run Beavend without deploying Redis.

## Decision
We decided to implement the `LimitStore` interface with two strategies:
1. **Redis / Valkey (Primary):** Evaluates all configured time windows for a user atomically in a **single round-trip using an embedded Lua script**. Keys use Redis hash tags (`lim:{project}:{env}:{user_hash}:{resource}:{window}`) ensuring cluster compatibility.
2. **PostgreSQL Fallback:** A database-backed implementation using atomic `INSERT ... ON CONFLICT DO UPDATE` queries for minimal single-node deployments.
3. **Failure Policy:** If the configured limit store is unreachable, the runtime defaults to **fail-closed** (`503 unavailable`), protecting the implementor against unexpected provider billing spikes.

Token limits employ an **optimistic reservation** model:
* Pre-request admission checks the user's remaining quota against an estimated upper bound (prompt tokens + requested `max_tokens`).
* Post-request asynchronous reconciliation records the actual provider token usage and credits or debits the counter.

## Consequences
### Positive
* Zero race conditions under concurrent burst traffic.
* Single Redis round-trip (<1 ms) for complex multi-window rate checking.
* Flexible deployment: Redis/Valkey for production scale, Postgres for minimal setups.

### Negative
* Token reservation requires post-request reconciliation, adding slight bookkeeping complexity.
* Redis failure in default configuration blocks requests (intended security tradeoff).
