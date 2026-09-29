# T068 — Control Plane Export/Import Endpoints & OpenAPI Contract

## Status
Completed

## Objective
Expose management REST endpoints for project export and import in `internal/control` and update `api/openapi.yaml`.

## Key Changes
1. Endpoints:
   - `GET /v1/admin/projects/{project_id}/environments/{env}/export`: outputs YAML file with `Content-Disposition: attachment; filename="{project}-{env}.yaml"`.
   - `POST /v1/admin/projects/{project_id}/environments/{env}/import`: accepts YAML/JSON body, supports `?dry_run=true` query param, returns `ImportResponse` with `DiffReport`.
2. Integration with `internal/control/handlers_projects.go` or dedicated `handlers_configcode.go`.
3. RBAC authorization: `admin` and `operator` permissions check.
4. Synchronous audit log entry for import events.
5. OpenAPI contract in `api/openapi.yaml`.
