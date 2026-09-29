-- name: CreateTool :one
INSERT INTO tools (id, project_id, environment, name, description, active_version, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
RETURNING *;

-- name: GetTool :one
SELECT * FROM tools
WHERE id = $1;

-- name: ListTools :many
SELECT * FROM tools
WHERE project_id = $1 AND environment = $2
ORDER BY name ASC;

-- name: UpdateToolActiveVersion :one
UPDATE tools
SET active_version = $2, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: CreateToolVersion :one
INSERT INTO tool_versions (
    id, tool_id, project_id, environment, version, side_effect,
    input_schema, identity_bindings, executor, downstream_auth,
    confirmation, "authorization", result_config, cache_config, is_active, created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, NOW())
RETURNING *;

-- name: GetToolVersion :one
SELECT * FROM tool_versions
WHERE tool_id = $1 AND version = $2;

-- name: GetActiveToolVersion :one
SELECT tv.*
FROM tool_versions tv
JOIN tools t ON t.id = tv.tool_id AND t.active_version = tv.version
WHERE t.id = $1;

-- name: ListActiveToolVersions :many
SELECT tv.*
FROM tool_versions tv
JOIN tools t ON t.id = tv.tool_id AND t.active_version = tv.version
WHERE t.project_id = $1 AND t.environment = $2;

-- name: ListToolVersions :many
SELECT * FROM tool_versions
WHERE tool_id = $1
ORDER BY version DESC;

-- name: DeleteTool :exec
DELETE FROM tools
WHERE id = $1;


-- name: UpsertToolSecret :one
INSERT INTO tool_secrets (
    id, project_id, environment, tool_id, secret_ref,
    encrypted_secret, nonce, key_id, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
ON CONFLICT (project_id, environment, tool_id, secret_ref)
DO UPDATE SET
    encrypted_secret = EXCLUDED.encrypted_secret,
    nonce = EXCLUDED.nonce,
    key_id = EXCLUDED.key_id,
    updated_at = NOW()
RETURNING *;

-- name: GetToolSecret :one
SELECT * FROM tool_secrets
WHERE project_id = $1 AND environment = $2 AND tool_id = $3 AND secret_ref = $4;

-- name: ListToolSecrets :many
SELECT * FROM tool_secrets
WHERE project_id = $1 AND environment = $2 AND tool_id = $3;

-- name: DeleteToolSecret :exec
DELETE FROM tool_secrets
WHERE project_id = $1 AND environment = $2 AND tool_id = $3 AND secret_ref = $4;
