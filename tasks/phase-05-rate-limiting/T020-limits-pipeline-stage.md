# T020 — Rate Limiting Pipeline Stage & HTTP Handling [Completed]

* **Status:** Completed

## Goal
Implement the rate limiting pipeline stage in `internal/runtime/stages` to enforce inference and token quotas on the request path, return RFC 7807 `429 rate_limited` responses with `Retry-After` headers, and record OpenTelemetry metrics.

## Depends On
- T015 (AuthZ Stage)
- T016 (Redis Limits)
- T017 (Tier Resolution)
- T018 (Token Reconciliation)

## Scope
* Implement `LimitsStage` (`core.Stage`):
  * Extract user key from `ReqState.Identity` (`project:env:hash(iss:sub)`).
  * Resolve rules for user via `TierResolver`.
  * Compute estimated tokens if token rules are present.
  * Evaluate rules atomically via `LimitStore.CheckAndDebit`.
  * If rejected:
    * Populate `state.Error` with `Code: "rate_limited"`, `Message: "rate limit exceeded"`, and `RetryAfter`.
    * Return `ErrRateLimited`.
    * Increment `beavend_limit_rejections_total` counter metric.
  * If allowed:
    * Attach token reservation to request context / state for post-request reconciliation.
    * Increment `beavend_limit_checks_total` metric.
  * On store failure:
    * If `fail_closed` is true: reject with `503 unavailable`.
    * If `fail_closed` is false: log warning and allow request.
* Table-driven tests for stage execution:
  * Allowed requests proceed.
  * Exceeded requests return `ErrRateLimited` with expected `RetryAfter`.
  * Anonymous / unauthenticated callers handling.
  * Store failure respecting `fail_closed` configuration.

## Acceptance Criteria
- [x] Pipeline stage returns `429 rate_limited` with positive integer `RetryAfter`.
- [x] Supports both Redis and Postgres `LimitStore` implementations.
- [x] OpenTelemetry metrics and structured logs emit attributes `app_id`, `resource`, `status`.
- [x] Table-driven tests pass.

## Verification
```bash
CGO_ENABLED=0 go test -race -v ./internal/runtime/stages/...
```
