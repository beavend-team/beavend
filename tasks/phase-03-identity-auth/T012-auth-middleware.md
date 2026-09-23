# T012 — Ingress Authentication Middleware and Error Mapping

## Goal
Implement the HTTP authentication middleware extracting Bearer tokens, performing verification, injecting `core.Identity` into `context.Context`, and returning RFC 7807 compliant `401 unauthenticated` errors on failure.

## Depends On
- T005
- T010
- T011

## Scope
* Implement `AuthMiddleware(registry *SnapshotRegistry, jwks *JWKSManager) func(http.Handler) http.Handler`.
* Extract bearer token from `Authorization: Bearer <token>`.
* Extract `X-Project-ID` and `X-Environment` headers.
* Resolve snapshot and project auth configuration.
* Validate token, normalize identity, and store `Identity` in request context via `context.WithValue`.
* Return standard error JSON with HTTP status 401 and code `unauthenticated` if token is missing, expired, or invalid.

## Requirements
- Respect request cancellation.
- Attach `request_id` to error responses.

## Constraints
- Do not log token contents or authorization headers.

## Acceptance Criteria
- [ ] Requests without Authorization header return `401 unauthenticated`.
- [ ] Requests with invalid or expired tokens return `401 unauthenticated`.
- [ ] Requests with valid token pass downstream with `core.Identity` accessible in context.

## Verification
```bash
go test -v ./internal/identity/middleware_test.go
```
