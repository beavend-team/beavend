-- name: CreatePrompt :one
INSERT INTO prompts (id, project_id, environment, name, description, active_version, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
RETURNING *;

-- name: GetPrompt :one
SELECT * FROM prompts
WHERE id = $1;

-- name: ListPrompts :many
SELECT * FROM prompts
WHERE project_id = $1 AND environment = $2
ORDER BY name ASC;

-- name: UpdatePromptActiveVersion :one
UPDATE prompts
SET active_version = $2, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: CreatePromptVersion :one
INSERT INTO prompt_versions (id, prompt_id, project_id, environment, version, template, variables, is_active, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
RETURNING *;

-- name: GetPromptVersion :one
SELECT * FROM prompt_versions
WHERE prompt_id = $1 AND version = $2;

-- name: GetActivePromptVersion :one
SELECT pv.*
FROM prompt_versions pv
JOIN prompts p ON p.id = pv.prompt_id AND p.active_version = pv.version
WHERE p.id = $1;

-- name: ListActivePromptVersions :many
SELECT pv.*
FROM prompt_versions pv
JOIN prompts p ON p.id = pv.prompt_id AND p.active_version = pv.version
WHERE p.project_id = $1 AND p.environment = $2;
