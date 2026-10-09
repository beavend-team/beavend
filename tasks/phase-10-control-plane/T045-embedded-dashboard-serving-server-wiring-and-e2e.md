# T045 - Embedded Dashboard Serving, Server Wiring & End-to-End Validation

* **Status:** Completed

## Goal
Embed the compiled dashboard static assets into the Go binary via `embed.FS`, wire all control-plane HTTP routes in `cmd/beavend`, update `api/openapi.yaml`, and validate the control plane with comprehensive unit and integration tests.

## Depends On
- T041, T042, T043, T044

## Scope
1. **Embedded Asset Filesystem (`internal/control/assets`):**
   - Use Go `embed.FS` to embed `dashboard/dist` assets.
   - HTTP file server with SPA routing fallback (serves `index.html` for non-file subpaths).
   - Graceful fallback placeholder when running without dashboard build in dev mode.
2. **Server Wiring (`cmd/beavend/server.go`, `cmd/beavend/main.go`):**
   - Mount control plane routes under `/v1/admin/*` when `Role == "control"` or `Role == "all"`.
   - Mount embedded dashboard at `/dashboard` and redirect `/` to `/dashboard`.
   - Wire initial admin user auto-seeding on server bootstrap if configured.
3. **OpenAPI Contract Update (`api/openapi.yaml`):**
   - Document all `/v1/admin/*` endpoints in the OpenAPI 3.1 specification.
4. **End-to-End & Integration Testing:**
   - Test admin login, session cookie / token generation.
   - Test project, app, prompt, model, tool, policy CRUD with RBAC enforcement.
   - Test snapshot revision increment and syncer notification on configuration change.
   - Test audit log recording and query.
   - Test embedded dashboard asset serving and SPA fallback.
5. **Update Roadmap:**
   - Mark Phase 10 as completed in `tasks/roadmap.md`.

## Acceptance Criteria
- [x] Single compiled Go binary serves both API and web dashboard.
- [x] Non-file requests to `/dashboard/*` return `index.html` (SPA routing works).
- [x] OpenAPI 3.1 contract fully documents all admin endpoints.
- [x] `go test -race -v ./internal/... ./cmd/...` passes 100%.
- [x] `golangci-lint run ./...` passes with 0 errors.

