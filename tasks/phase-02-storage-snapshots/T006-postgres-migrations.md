# T006 — PostgreSQL Schema Migrations with Goose

## Goal
Author versioned PostgreSQL DDL migrations using `goose` to define tenant, configuration, and operational tables in `internal/store/migrations`.

## Depends On
- T001
- T002

## Scope
Create SQL migrations for:
* `projects` and `environments`.
* `ai_apps`, `ai_app_versions`.
* `prompts`, `prompt_versions` (immutable).
* `policies`, `policy_versions` (immutable).
* `tools`, `tool_versions` (immutable), `tool_secrets` (encrypted).
* `models`, `provider_configs` (encrypted).
* `guardrails`, `guardrail_versions`.
* `project_revisions` (revision counter for snapshot cache invalidation).

## Requirements
- All tenant tables must include `project_id` and `environment` columns.
- Indexes on foreign keys and `(project_id, environment)`.
- Use `TIMESTAMPTZ` for all timestamp columns.

## Constraints
- Do not introduce ORM-specific syntax; pure PostgreSQL 15+ compatible SQL.

## Acceptance Criteria
- [x] Goose migrations execute cleanly up and down against a clean PostgreSQL container.
- [x] Schema matches all entities outlined in Architecture §9.1.

## Verification
```bash
goose -dir internal/store/migrations postgres "$CORTEXBASE_DATABASE_URL" up
goose -dir internal/store/migrations postgres "$CORTEXBASE_DATABASE_URL" down
```
