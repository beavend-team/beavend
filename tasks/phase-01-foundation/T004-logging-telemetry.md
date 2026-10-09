# T004 - Structured Logging and OpenTelemetry Setup

## Goal
Set up structured JSON logging using Go `slog` and OpenTelemetry tracing/metrics in `internal/telemetry`.

## Depends On
- T001
- T003

## Scope
* Configure `slog` default logger formatting JSON to `stdout` with configurable levels (`debug`, `info`, `warn`, `error`).
* Implement standard log attributes helper attaching `request_id`, `project_id`, and `environment`.
* Initialize OpenTelemetry TracerProvider and MeterProvider with Prometheus exporter endpoint.

## Requirements
- Slog handler must redact sensitive keys: `password`, `token`, `secret`, `authorization`, `api_key`.
- Prometheus metrics endpoint mounted at `/metrics`.

## Constraints
- Logging must not block request goroutines.

## Acceptance Criteria
- [x] Structured JSON output contains timestamp, level, caller, and attributes.
- [x] Sensitive headers and tokens are verified redacted in unit tests.
- [x] Prometheus metrics endpoint returns standard runtime metrics.

## Verification
```bash
go test -v ./internal/telemetry/...
```
