# T042 — Management API: Core Resources CRUD & Snapshot Invalidation

* **Status:** Completed

## Goal
Implement RESTful management endpoints for configuration resources (projects, environments, AI apps, prompts, models/routings, tools, policies) and guarantee atomic snapshot revision invalidation across runtime instances upon every configuration change.

## Depends On
- T041 (Admin Auth & RBAC)
- Phase 2 (Persistent Storage & Snapshot Compiler)

## Scope
1. **Management Endpoints (`internal/control/handlers`):**
   - **Projects & Environments:**
     - `GET /v1/admin/projects` — list projects
     - `POST /v1/admin/projects` — create project
     - `GET /v1/admin/projects/{project_id}` — get project
     - `PUT /v1/admin/projects/{project_id}` — update project
     - `DELETE /v1/admin/projects/{project_id}` — delete project (cascades)
     - `GET /v1/admin/projects/{project_id}/environments` — list environments
     - `POST /v1/admin/projects/{project_id}/environments` — create environment
     - `DELETE /v1/admin/projects/{project_id}/environments/{name}` — delete environment
   - **AI Apps:**
     - `GET /v1/admin/apps` — list AI apps in project/env
     - `POST /v1/admin/apps` — create AI app & initial version
     - `GET /v1/admin/apps/{id}` — get AI app and active version
     - `PUT /v1/admin/apps/{id}` — update app metadata / create new version
     - `DELETE /v1/admin/apps/{id}` — delete AI app
   - **Prompts:**
     - `GET /v1/admin/prompts` — list prompts
     - `POST /v1/admin/prompts` — create prompt & initial version
     - `GET /v1/admin/prompts/{id}` — get prompt details & versions
     - `POST /v1/admin/prompts/{id}/versions` — create/publish new prompt version
   - **Models & Routings:**
     - `GET /v1/admin/models` — list model routings
     - `PUT /v1/admin/models/{logical_model}` — upsert logical model routing
     - `DELETE /v1/admin/models/{logical_model}` — delete routing
     - `GET /v1/admin/providers` — list provider configs
     - `PUT /v1/admin/providers/{provider}` — upsert provider configuration with encrypted API key
   - **Tools:**
     - `GET /v1/admin/tools` — list tools
     - `POST /v1/admin/tools` — create tool & version (JSON schema, identity bindings, executor)
     - `GET /v1/admin/tools/{id}` — get tool details
     - `PUT /v1/admin/tools/{id}` — update tool / create version
     - `DELETE /v1/admin/tools/{id}` — delete tool
     - `PUT /v1/admin/tools/{id}/secrets/{secret_ref}` — upsert encrypted tool secret
   - **Policies:**
     - `GET /v1/admin/policies` — list policies
     - `POST /v1/admin/policies` — create policy & compile/validate CEL expression
     - `GET /v1/admin/policies/{id}` — get policy details
     - `PUT /v1/admin/policies/{id}` — update policy / publish version
2. **Snapshot Invalidation Mechanism:**
   - Whenever any entity in `(project_id, environment)` is modified, the handler:
     1. Commits the transaction to PostgreSQL.
     2. Calls `IncrementProjectRevision(ctx, project_id, environment)`.
     3. Emits invalidation event (via Redis pub/sub channel `beavend:config:invalidations` or PostgreSQL `NOTIFY beavend_config_updates`).
     4. Records an audit log entry for the action.
3. **Validation & CEL Safety:**
   - Pre-validate JSON schemas for tools using `santhosh-tekuri/jsonschema`.
   - Pre-compile CEL expressions for policies using Google CEL compiler before saving to reject syntax/type errors immediately.

## Acceptance Criteria
- [ ] CRUD endpoints adhere to REST conventions and handle headers `X-Project-ID` and `X-Environment`.
- [ ] Role-based authorization enforced (`viewer` cannot mutate, `editor` can mutate resources, `owner` can mutate projects/providers).
- [ ] Revision number increases on every mutation and invalidation notification is sent.
- [ ] CEL and JSON schema validation rejects malformed configurations with descriptive 400 errors.
- [ ] Unit tests for all resource endpoints.
