# T008 — In-Memory Configuration Snapshot Compiler and Atomic Swap

## Goal
Implement the configuration compiler in `internal/snapshot` that compiles raw database models into pre-compiled in-memory `Snapshot` objects and swaps them atomically.

## Depends On
- T002
- T007

## Scope
* Define `Snapshot` struct containing:
  * Compiled CEL programs for policies.
  * Parsed prompt templates.
  * Validated JSON Schemas for tools.
  * Resolved logical model mappings and limit rules.
* Implement `Compiler.Compile(ctx, projectID, env) (*Snapshot, error)` loading data from `internal/store`.
* Implement `SnapshotRegistry` managing `atomic.Pointer[*Snapshot]` per `(project, env)`.

## Requirements
- Compiling a snapshot must validate that all referenced tools, prompts, and policies exist and parse without error.
- Accessing a snapshot via `registry.Get(projectID, env)` must be a lock-free, zero-allocation pointer read.

## Constraints
- The request execution path must only call `registry.Get` and never trigger a database query.

## Acceptance Criteria
- [x] Compiler converts DB records into a valid `Snapshot`.
- [x] Swapping snapshots is thread-safe under high concurrent read load.
- [x] In-flight requests holding an older snapshot reference are unaffected by a new snapshot swap.

## Verification
```bash
go test -race -v ./internal/snapshot/...
```
