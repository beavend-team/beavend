# T082 - Snapshot Compiler: Rule Compilation & Validation

* **Status:** Completed

## Goal
Compile routes into the in-memory snapshot and reject invalid config at publish time.

## Scope
1. Compile `ModelRoutes` (including optional CEL `expr` via `internal/policy`) in `internal/snapshot/compiler.go`.
2. Validate: `default` present; every `use` exists; rule target supports the `input_has`/`output` modalities when capability metadata is declared; fallbacks are capability-compatible.
3. Legacy compatibility: apps without `model_routes` use `ModelRoutingID` as default.
4. Table-driven tests for valid and invalid configs.

## Acceptance Criteria
- [x] Invalid config blocks publish with a clear error.
- [x] No DB access at request time.
