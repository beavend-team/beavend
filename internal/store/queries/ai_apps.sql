-- name: CreateAIApp :one
INSERT INTO ai_apps (id, project_id, environment, name, description, active_version, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
RETURNING *;

-- name: GetAIApp :one
SELECT * FROM ai_apps
WHERE id = $1;

-- name: ListAIApps :many
SELECT * FROM ai_apps
WHERE project_id = $1 AND environment = $2
ORDER BY name ASC;

-- name: UpdateAIAppActiveVersion :one
UPDATE ai_apps
SET active_version = $2, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: DeleteAIApp :exec
DELETE FROM ai_apps
WHERE id = $1;

-- name: CreateAIAppVersion :one
INSERT INTO ai_app_versions (
    id, app_id, project_id, environment, version, access_policy,
    prompt_id, prompt_version, model_routing_id, tool_ids,
    limits, cache_config, guardrails, response_schema, is_active, created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, NOW())
RETURNING *;

-- name: GetAIAppVersion :one
SELECT * FROM ai_app_versions
WHERE app_id = $1 AND version = $2;

-- name: GetActiveAIAppVersion :one
SELECT aav.*, a.name, a.description
FROM ai_app_versions aav
JOIN ai_apps a ON a.id = aav.app_id AND a.active_version = aav.version
WHERE a.id = $1;

-- name: ListActiveAIAppVersions :many
SELECT aav.*, a.name, a.description
FROM ai_app_versions aav
JOIN ai_apps a ON a.id = aav.app_id AND a.active_version = aav.version
WHERE a.project_id = $1 AND a.environment = $2;

-- name: ListAIAppVersions :many
SELECT * FROM ai_app_versions
WHERE app_id = $1
ORDER BY version DESC;

