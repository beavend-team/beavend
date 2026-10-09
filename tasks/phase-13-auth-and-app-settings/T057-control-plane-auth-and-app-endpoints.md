# T057 - Control Plane AuthConfig and AI App Management Endpoints

* **Status:** Completed

## Goal
Implement administrative REST endpoints in `internal/control` to manage environment Identity Provider configurations (`AuthConfig`), test client JWT tokens, and support full AI App updates (including Limits, Guardrails, Cache, Tools, and Schemas).

## Scope
1. **AuthConfig Endpoints (`internal/control/handlers_projects.go` or `handlers_auth_config.go`):**
   - `GET /v1/admin/projects/{project_id}/environments/{name}/auth` -> return `core.AuthConfig`.
   - `PUT /v1/admin/projects/{project_id}/environments/{name}/auth` -> validate, save `auth_config`, increment project revision, and broadcast snapshot invalidation.
   - `POST /v1/admin/auth/test-token` -> accepts `{ token, auth_config? }`, runs verification against the environment or provided config, and returns `{ valid: bool, subject: string, issuer: string, audience: string, claims: map, error?: string }`.
2. **AI App Endpoints Enhancements (`internal/control/handlers_apps.go`):**
   - Enhance `HandleGetAIApp` to return full active version details (`limits`, `guardrails`, `cache_config`, `tool_ids`, `response_schema`, `prompt_id`, `model_routing_id`, `access_policy`).
   - Enhance `HandleUpdateAIApp` to accept and persist updated `limits`, `guardrails`, `cache_config`, `tool_ids`, `response_schema`, creating a new version and invalidating snapshot.
3. **Unit Tests:**
   - Test AuthConfig CRUD, token inspection endpoint, and App versioning with full configurations.

## Acceptance Criteria
- [x] Endpoints mounted and functional under `/v1/admin/...`.
- [x] Updating AuthConfig invalidates the project snapshot.
- [x] Test token endpoint accurately validates valid/invalid JWTs and returns extracted claims.
- [x] `go test ./internal/control/...` passes cleanly.
