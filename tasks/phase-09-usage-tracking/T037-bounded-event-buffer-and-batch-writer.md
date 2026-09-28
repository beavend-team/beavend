# T037 — Bounded Event Buffer & Batch Writer

* **Status:** Completed

## Goal
Implement a thread-safe, bounded in-process event channel with configurable backpressure policies, batching, and periodic flushing in `internal/usage`.

## Depends On
- T036 (Usage Schema)

## Scope
1. **Event Types:**
   - Define `LogEvent` and `UsageRecord` domain types in `internal/usage` (or `internal/core`).
2. **Bounded Buffer (`internal/usage/buffer.go`):**
   - Configurable buffer capacity (default 10,000 events).
   - Ingestion methods: `Enqueue(event)` (non-blocking).
   - Backpressure modes:
     - `BackpressureDropNewest`: Drop event when buffer is full, increment metric counter.
     - `BackpressureBlockTimeout`: Short timeout before dropping.
   - Separate handling or priority for billing-critical usage vs debug logs.
3. **Batch Writer (`internal/usage/batch_writer.go`):**
   - Accumulate events up to `batch_size` (e.g. 100) or `flush_interval` (e.g. 500ms).
   - Batch insert into PostgreSQL using pgx transactions / COPY protocol or batched queries.
   - Retries on transient database errors with exponential backoff.
4. **Lifecycle & Graceful Shutdown:**
   - Worker goroutine loop listening on ticker and channel.
   - `Close(ctx)` cleanly flushes remaining buffered events before returning or until context deadline.
5. Unit and concurrency tests verifying buffer overflow handling, batch flush timers, and clean shutdown.

## Acceptance Criteria
- [ ] Enqueue never blocks the caller indefinitely under normal or congested conditions.
- [ ] Batches are flushed when size limit is hit or ticker fires.
- [ ] Shutdown drains all buffered events within context deadline.
- [ ] Zero race conditions (`go test -race`).
