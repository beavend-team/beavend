# ADR-008: Asynchronous Bounded Usage Pipeline and Partitioned Storage

## Status
Accepted

## Context
High-volume AI application backends generate substantial operational telemetry, request logs, and token usage accounting events. Because end users are waiting on interactive streaming (SSE) or unary responses, the hot request-response path must not perform synchronous database writes. Furthermore, storing millions of request metadata records in PostgreSQL requires partitioning by time so that retention pruning (dropping old partitions) is instant and non-blocking, and indexing remains compact and performant.

Additionally, to prevent database overload under burst or sustained traffic, the ingestion pipeline must run through an in-process bounded channel with explicit backpressure policies (durable buffering vs sampling/dropping for non-billing logs) and batch writes.

## Decision
1. **Asynchronous Bounded Channel Ingestion:**
   * Handlers and pipeline stages never write usage or request logs synchronously to PostgreSQL.
   * Completed requests (both unary chat and streaming responses) emit an event into an in-process bounded channel (`chan UsageEvent`, default capacity e.g. 10,000).
   * If the buffer is full, configurable backpressure policies apply: billing-critical usage events are preserved while verbose non-critical logs may be sampled or dropped with Prometheus counters incremented.
2. **Batch Persistence:**
   * A background worker pool drains the bounded buffer and flushes batches to PostgreSQL when either batch size threshold (e.g. 100 items) or flush interval (e.g. 500ms) triggers.
   * Supports graceful draining on server shutdown with a dedicated context timeout.
3. **Partitioned Storage Schema:**
   * `request_logs` and `usage_events` tables are partitioned by range on `created_at` (daily or weekly partitions).
   * Default schema records operational metadata: request ID, project, environment, AI app, user identity (subject + issuer), model deployment, token counts (input, output, total), latency, status, error code, and tool call summaries.
   * Request bodies and raw message content are excluded by default for privacy and storage efficiency.
4. **Aggregated Rollups (`usage_rollups`):**
   * A background aggregator computes hourly and daily rollups grouped by `(project_id, environment, ai_app_id, user_id, logical_model, provider)` so dashboard analytics queries do not scan large raw event partitions.
5. **Token Reconciliation Integration:**
   * Upon request completion, actual token consumption is reconciled with pre-request estimates on the configured `LimitStore` asynchronously.

## Consequences
### Positive
* Hot path request latency is completely decoupled from PostgreSQL write latency.
* Database write operations are batched, significantly reducing transaction overhead and lock contention.
* Time-based partitioning enables fast retention enforcement (`DROP TABLE partition`) without vacuums or row-level deletion locking.
* Dashboard analytics queries hit fast, compact rollup tables.

### Negative
* In-process buffers risk minor event loss if the server crashes abruptly (mitigated at scale via Redis Streams as documented in architecture).
* Background rollups introduce slight lag (minutes) before aggregated analytics reflect raw events.

## Alternatives Considered
* **Synchronous writes inside handler:** Rejected because database stalls would degrade client response latency and breach the hot-path latency SLAs.
* **Unpartitioned single table with cron delete:** Rejected because row-level `DELETE FROM` on millions of rows causes severe write amplification and table bloat in PostgreSQL.
