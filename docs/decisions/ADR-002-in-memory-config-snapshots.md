# ADR-002: In-Memory Compiled Configuration Snapshots on Hot Path

## Status
Accepted

## Context
The Beavend runtime plane is on the critical path of end-user application interactions. The PRD imposes a strict non-functional performance requirement: Beavend must add under 30 ms p95 overhead (excluding provider and downstream tool latency).

AI requests require extensive configuration data: project settings, AI App metadata, active prompt versions, logical model mappings, CEL authorization policies, JSON schemas for tools, and rate limit rules. Querying PostgreSQL on every request to fetch and parse this configuration would introduce significant database connection overhead, query latency, and a severe scaling bottleneck.

## Decision
We decided that **the runtime plane will perform zero database queries on the request hot path for configuration**.

Instead:
1. All project configuration is compiled into an immutable in-memory data structure (`Snapshot`).
2. Expressions (CEL), schemas (JSON Schema), and prompt templates are pre-compiled and validated when the snapshot is created, not per request.
3. Snapshots are stored in an in-memory map keyed by `(project_id, environment)` and swapped atomically using Go's `atomic.Pointer`.
4. When configuration changes in the control plane, a notification is broadcast via Redis pub/sub (with PostgreSQL `LISTEN`/`NOTIFY` fallback). Runtime instances fetch the revision, compile the new snapshot, and swap it into service.
5. In-flight requests pin the snapshot reference they began with, ensuring consistent behavior throughout the entire request lifecycle.

## Consequences
### Positive
* Request overhead for configuration resolution is pure in-memory pointer dereference (sub-millisecond).
* Runtime instances continue serving AI traffic without interruption even if PostgreSQL experiences temporary outages or high maintenance load.
* Predictable and verifiable p95 latency.

### Negative
* Configuration updates have a brief propagation delay (typically <1 second) across runtime instances.
* Memory usage on runtime nodes scales with the number of active projects and configured tools/prompts.
