# T002 — Core Domain Interfaces and Models

## Goal
Define the fundamental domain entities, data contracts, and component interfaces in `internal/core`.

## Depends On
- T001

## Scope
Define core types without external framework dependencies:
* `Identity`: Normalized end-user identity representation.
* `AIApp`, `PromptVersion`, `ModelRouting`, `ToolDefinition`, `PolicyDefinition`.
* `Snapshot`: Compiled configuration container.
* `ProviderAdapter`: Interface for LLM provider operations.
* `Executor`: Interface for tool execution.
* `LimitStore`: Interface for quota and rate-limit tracking.
* `Stage`: Request pipeline stage interface.

## Requirements
- `Identity` must include: `Project`, `Environment`, `Issuer`, `Subject`, `Audience`, `Claims`.
- `ProviderAdapter` must define `Chat`, `Stream`, `Embed`, and `Health` methods.
- `Stage` interface must define `Handle(ctx context.Context, state *ReqState) error`.

## Constraints
- `internal/core` must have ZERO dependencies on other `internal/` packages.
- All structs must use JSON and YAML tags where applicable.

## Acceptance Criteria
- [x] All primary abstractions from Architecture §13 are represented as Go interfaces in `internal/core`.
- [x] Domain structs compile cleanly with unit test assertions verifying interface conformance.

## Verification
```bash
go test -v ./internal/core/...
```
