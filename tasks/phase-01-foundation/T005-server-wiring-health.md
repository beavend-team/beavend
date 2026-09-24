# T005 — HTTP Server Wiring, Role Flags, and Health Probes

## Goal
Implement the main application entrypoint in `cmd/beavend` with role-based routing, signal handling, graceful shutdown, and `/healthz` / `/readyz` probes.

## Depends On
- T001
- T002
- T003
- T004

## Scope
* Command line parsing for `--role` (`all`, `runtime`, `control`).
* Wire HTTP server using `chi` router.
* Implement `/healthz` (liveness) returning `200 OK`.
* Implement `/readyz` (readiness) checking snapshot readiness and dependencies.
* Handle `SIGINT` and `SIGTERM` for graceful shutdown with a 15-second drain window.

## Requirements
- Support setting read, write, and idle timeouts on `http.Server`.
- Ensure SSE streaming endpoints bypass default write timeouts where appropriate.

## Constraints
- Do not mount control routes on `--role=runtime` or runtime routes on `--role=control`.

## Acceptance Criteria
- [x] Binary builds successfully: `go build -o beavend ./cmd/beavend`.
- [x] Binary starts and responds with `200 OK` on `/healthz`.
- [x] Role flag controls route mounting as expected.
- [x] Graceful shutdown drains connections cleanly upon signal.

## Verification
```bash
go run ./cmd/beavend --role=all &
PID=$!
sleep 1
curl -f http://localhost:8080/healthz
kill -TERM $PID
wait $PID
```
