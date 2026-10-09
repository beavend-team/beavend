-- name: CreateAIApp :one
INSERT INTO ai_apps (id, project_id, environment, name, description, active_version, auth_config, model_routes, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
RETURNING *;

-- name: GetAIApp :one
SELECT * FROM ai_apps
WHERE id = $1;

-- name: ListAIApps :many
SELECT * FROM ai_apps
WHERE project_id = $1 AND environment = $2
ORDER BY name ASC;

-- name: UpdateAIApp :one
UPDATE ai_apps
SET name = $2,
    description = $3,
    auth_config = $4,
    model_routes = $5,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateAIAppActiveVersion :one
UPDATE ai_apps
SET active_version = $2,
    auth_config = $3,
    model_routes = $4,
    updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: DeleteAIApp :exec
DELETE FROM ai_apps
WHERE id = $1;

-- name: CreateAIAppVersion :one
INSERT INTO ai_app_versions (
    id, app_id, project_id, environment, version, access_policy,
    prompt_id, prompt_version, model_routing_id, tool_ids,
    limits, cache_config, guardrails, response_schema, is_active, memory_config, search_config, created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, NOW())
RETURNING *;

-- name: GetAIAppVersion :one
SELECT * FROM ai_app_versions
WHERE app_id = $1 AND version = $2;

-- name: GetActiveAIAppVersion :one
SELECT aav.*, a.name, a.description, a.auth_config, a.model_routes
FROM ai_app_versions aav
JOIN ai_apps a ON a.id = aav.app_id AND a.active_version = aav.version
WHERE a.id = $1;

-- name: ListActiveAIAppVersions :many
SELECT aav.*, a.name, a.description, a.auth_config, a.model_routes
FROM ai_app_versions aav
JOIN ai_apps a ON a.id = aav.app_id AND a.active_version = aav.version
WHERE a.project_id = $1 AND a.environment = $2;

-- name: ListAIAppVersions :many
SELECT * FROM ai_app_versions
WHERE app_id = $1
ORDER BY version DESC;

-- name: CreateAppAPIKey :one
INSERT INTO app_api_keys (
    id, app_id, project_id, environment, name, key_prefix, key_hash, created_at, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), $8)
RETURNING *;

-- name: ListAppAPIKeys :many
SELECT id, app_id, project_id, environment, name, key_prefix, created_at, last_used_at, expires_at
FROM app_api_keys
WHERE project_id = $1 AND environment = $2 AND app_id = $3
ORDER BY created_at DESC;

-- name: DeleteAppAPIKey :exec
DELETE FROM app_api_keys
WHERE id = $1 AND app_id = $2;

-- name: GetAppAPIKeyByHash :one
SELECT * FROM app_api_keys
WHERE key_hash = $1 AND (expires_at IS NULL OR expires_at > NOW());

-- name: UpdateAppAPIKeyLastUsed :exec
UPDATE app_api_keys
SET last_used_at = NOW()
WHERE id = $1;


