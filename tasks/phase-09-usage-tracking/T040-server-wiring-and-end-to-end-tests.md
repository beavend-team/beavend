# T040 - Server Wiring & End-to-End Validation

* **Status:** Completed

## Goal
Wire the usage tracker, batch writer, and rollup aggregator into `cmd/beavend` for `--role=runtime`, `--role=control`, and `--role=all`, ensuring clean initialization and graceful shutdown.

## Depends On
- T036, T037, T038, T039

## Scope
1. **Configuration Integration:**
   - Update `internal/config/config.go` with usage pipeline parameters:
     - `usage.buffer_size` (default: 10000)
     - `usage.batch_size` (default: 100)
     - `usage.flush_interval` (default: 500ms)
     - `usage.rollup_interval` (default: 1m)
2. **Server Wiring (`cmd/beavend/main.go`):**
   - Initialize `BatchWriter` when database is available.
   - Inject usage tracker into `runtime.Handler`.
   - Start rollup aggregator in control plane or single-node (`role=all` or `role=control`).
   - Register shutdown handlers ensuring buffered events are flushed before server termination.
3. **Integration & End-to-End Tests:**
   - Integration test running real request flows through `runtime.Handler` -> async buffer -> batch writer -> PostgreSQL.
   - Verify zero race conditions (`go test -race`).
   - Verify `golangci-lint` passes with zero warnings.

## Acceptance Criteria
- [ ] Server boots with usage pipeline enabled.
- [ ] Requests emit usage records without slowing down response latency.
- [ ] Graceful shutdown drains all buffered events cleanly.
- [ ] All unit and integration tests pass with race detector enabled.
