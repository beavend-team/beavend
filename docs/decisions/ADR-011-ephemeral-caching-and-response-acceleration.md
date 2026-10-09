# ADR-011: Ephemeral Caching and Response Acceleration

## Status
Accepted

## Context
Generative AI applications frequently process repetitive end-user inquiries and idempotent tool executions (e.g. FAQ lookups, weather forecasts, static product catalog queries). Invoking upstream large language models or downstream external APIs on every identical request incurs substantial token costs, increases latency from hundreds of milliseconds to multiple seconds, and introduces operational fragility.

In accordance with PRD §23, Architecture §7.3, and the zero-DB-read hot-path invariant, Beavend requires a distributed, identity-isolated ephemeral caching tier supporting response caching and tool execution caching with strict privacy lifecycle controls.

## Decision
1. **Multi-Backend Caching Tier:**
   - Implement `core.CacheStore` in `internal/cache` with `RedisCacheStore` (backed by Redis/Valkey) for distributed multi-replica clusters and `MemoryCacheStore` with concurrency-safe TTL maps for single-node or local deployments.
   - Support `PurgeableCacheStore` interface for bulk pattern scanning (`DeletePattern`) and user-scoped purging (`DeleteSubjectKeys`).

2. **Standardized Keying & Cryptographic Hashing:**
   - Keys strictly adhere to Architecture §7.3 to guarantee multi-tenant and cross-user isolation:
     - **Response Cache:** `rc:{project}:{env}:{scope}:{hash}` where `scope` is either `shared` or `user:{subject}`.
     - **Tool Result Cache:** `tc:{project}:{env}:{user}:{tool}:{argshash}` where `user` is caller subject.
   - Request and argument hashing use deterministic JSON canonicalization followed by SHA-256 digests (`cache.ComputeRequestHash`, `cache.ComputeToolArgsHash`).

3. **Pipeline Placement & Hot-Path Execution:**
   - `CacheStage` executes in the runtime pipeline after ingress authentication, rate limits, and input guardrails, but before prompt templating, model routing, and execution stages.
   - On **Cache Hit**:
     - Synchronous unary completions return cached `core.ChatResponse` directly with `X-Cache: HIT` header.
     - Streaming requests replay cached deltas over the SSE stream channel (`state.StreamChan`), followed by usage and done events, preserving client streaming expectations without upstream model invocations.
     - Downstream stages (`PromptStage`, `ModelRoutingStage`, `ExecutionStage`) detect pre-populated responses and skip execution immediately.
   - On **Cache Miss**:
     - Requests execute normally with `X-Cache: MISS`.
     - Upon successful completion without errors or confirmation pauses, `Handler` persists the response to the `CacheStore` with the configured TTL (`AIApp.Cache.TTL`).

4. **Tool Result Caching in Bounded Agent Loop:**
   - Before executing any server-side tool where `ToolDefinition.Cache.Enabled = true`, `agent.Loop` checks `CacheStore` using `tc:{project}:{env}:{user}:{tool}:{argshash}`.
   - On hit: returns cached sanitized result immediately without network execution.
   - On miss: executes tool via configured executor, sanitizes untrusted output, and caches the result with `ToolDefinition.Cache.TTL` (default 300s).

5. **Privacy Compliance & Subject Purge:**
   - When an administrator issues a privacy erasure request (`DELETE /v1/admin/subjects/{issuer}/{subject}` per PRD §25.3), `control.Service.PurgeSubjectCache` invokes `DeleteSubjectKeys`, permanently purging all user-scoped response and tool cache keys from Redis/memory.

6. **Observability:**
   - `beavend_cache_requests_total` metric tracks lookups partitioned by `cache_type` ("response", "tool") and `status` ("hit", "miss").
   - HTTP response headers convey cache disposition (`X-Cache: HIT` / `X-Cache: MISS`).

## Consequences
### Positive
* Sub-millisecond response latency and zero model token cost for cached prompts and idempotent tool calls.
* Guarantees strict user isolation with default `scope: user` preventing cross-user data leakage.
* Seamless streaming client acceleration with faithful SSE delta replay.
* Zero-DB-read hot path preserved: cache configuration is read entirely from in-memory snapshots.
* Full GDPR/CCPA subject erasure support through atomic pattern purges.

### Negative
* In-memory cache fallback consumes process RAM and is not shared across replicas (Redis/Valkey required for multi-replica deployments).
* Dynamic prompts containing timestamps or per-second variables will naturally miss the response cache (tool result caching can still accelerate these requests).

## Alternatives Considered
* **Semantic Vector Cache on pgvector:** Deferred to v0.3 milestone as an opt-in extension. Exact cryptographic hashing provides deterministic safety, microsecond speed, and zero embeddings cost for identical queries.
* **HTTP Reverse Proxy Caching (Varnish/Nginx):** Rejected because Beavend requires tenant isolation, claims-based user scoping, streaming SSE delta replay, and downstream tool call caching that external edge caches cannot inspect or orchestrate.
