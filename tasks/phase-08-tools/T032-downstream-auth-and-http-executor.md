# T032: Downstream Auth Strategies & HTTP Tool Executor

## Status
Completed

## Context & Objectives
Implement the `core.Authenticator` strategies and the hardened `HTTPExecutor` implementing `core.Executor`.

### Authenticator Modes:
1. `forward_user_token`: Extracts the caller's JWT token (or Bearer header) from context/identity and attaches it to the outbound request in the configured header (default: `Authorization: Bearer <token>`).
2. `service_token`: Attaches a static service token or secret reference (e.g. `Authorization: Bearer <secret>`).
3. `signed_headers`: Computes an HMAC-SHA256 signature across the request method, path, timestamp, and body using a shared secret, and injects `X-Beavend-Signature`, `X-Beavend-Timestamp`, and `X-User-Subject`.
4. `token_exchange`: Placeholder/strategy for RFC 8693 token exchange.

### HTTP Executor:
* Uses `SafeDialer` from T031.
* URL templating: substitutes path/query parameters (e.g. `https://api.example.com/users/{user_id}/orders`) with tool arguments.
* Handles HTTP methods (GET, POST, PUT, DELETE, PATCH).
* For POST/PUT/PATCH, formats request body as JSON with arguments.
* Enforces configured `timeout_ms` and retry attempts with backoff.
* Propagates `context.Context` cancellation so client abort halts outbound requests.
* Propagates `X-Request-Id` header.

## Deliverables
* `internal/tools/auth.go`: `Authenticator` interface and implementations for all 4 downstream auth modes.
* `internal/tools/http.go`: `HTTPExecutor` implementing `core.Executor`.
* `internal/tools/http_test.go`: Unit tests with mock HTTP test servers for all auth modes, retries, URL templating, and timeouts.
