# T009 - Redis Pub/Sub Revision Synchronization Listener

## Goal
Implement the real-time configuration revision invalidation listener using Redis pub/sub with periodic polling backstop in `internal/snapshot/sync`.

## Depends On
- T008

## Scope
* Implement revision publisher triggered when configuration is saved in the control plane.
* Implement pub/sub subscriber listening on channel `cfg:{project}`.
* Upon receiving a notification (`project_id`, `environment`, `new_revision`):
  1. Trigger asynchronous compilation of the new snapshot.
  2. Swap the compiled snapshot into `SnapshotRegistry`.
* Implement a periodic polling backstop (every 10 seconds) to detect any missed pub/sub messages.

## Requirements
- Gracefully handle Redis disconnections with automatic reconnection.
- If snapshot compilation fails (due to invalid CEL or schema), reject the swap, log an error, and retain the previous valid snapshot.

## Constraints
- A failed compilation must never crash the runtime or leave the registry in an inconsistent state.

## Acceptance Criteria
- [x] Publishing a revision event causes connected runtime instances to update their in-memory snapshot within 500 ms.
- [x] If Redis is unavailable, the periodic polling mechanism successfully updates the snapshot from the database.

## Verification
```bash
go test -tags=integration -race -v ./internal/snapshot/sync/...
```
