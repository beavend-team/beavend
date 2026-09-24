# T001 — Go Module Initialization, Dependency Pinning, and Project Layout

## Goal
Initialize the Go module, establish internal package boundaries, and pin techstack-approved dependencies in `go.mod`.

## Depends On
None

## Scope
* Initialize Go module `github.com/beavend/beavend` with Go 1.22+.
* Create root directory structure as specified in Architecture §14.
* Configure `golangci-lint` with `depguard` rules enforcing modular monolith inward dependency boundaries.

## Requirements
- Pin dependencies in `go.mod`: `github.com/go-chi/chi/v5`, `github.com/lestrrat-go/jwx/v2`, `github.com/google/cel-go`, `github.com/santhosh-tekuri/jsonschema/v5`, `github.com/jackc/pgx/v5`, `github.com/redis/go-redis/v9`, `go.opentelemetry.io/otel`.
- Configure `.golangci.yml` with `depguard` forbidding any package outside `cmd` from importing `internal/control` or `internal/runtime`.

## Constraints
- Do not introduce unapproved libraries (no ORMs, no heavy web frameworks).
- Enforce Go standard library compatibility.

## Acceptance Criteria
- [x] `go.mod` and `go.sum` are valid and clean.
- [x] All required package directories exist under `internal/`.
- [x] `.golangci.yml` is configured and passes when running `golangci-lint run ./...`.

## Verification
```bash
go mod tidy
golangci-lint run ./...
```
