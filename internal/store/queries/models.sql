-- name: UpsertProviderConfig :one
INSERT INTO provider_configs (
    id, project_id, environment, provider_name, base_url,
    encrypted_api_key, nonce, key_id, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
ON CONFLICT (project_id, environment, provider_name)
DO UPDATE SET
    base_url = EXCLUDED.base_url,
    encrypted_api_key = EXCLUDED.encrypted_api_key,
    nonce = EXCLUDED.nonce,
    key_id = EXCLUDED.key_id,
    updated_at = NOW()
RETURNING *;

-- name: GetProviderConfig :one
SELECT * FROM provider_configs
WHERE project_id = $1 AND environment = $2 AND provider_name = $3;

-- name: ListProviderConfigs :many
SELECT * FROM provider_configs
WHERE project_id = $1 AND environment = $2
ORDER BY provider_name ASC;

-- name: DeleteProviderConfig :exec
DELETE FROM provider_configs
WHERE project_id = $1 AND environment = $2 AND provider_name = $3;

-- name: UpsertModelRouting :one
INSERT INTO model_routings (
    id, project_id, environment, logical_model, deployment,
    adapter, fallback_deployments, created_at, updated_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW())
ON CONFLICT (project_id, environment, logical_model)
DO UPDATE SET
    deployment = EXCLUDED.deployment,
    adapter = EXCLUDED.adapter,
    fallback_deployments = EXCLUDED.fallback_deployments,
    updated_at = NOW()
RETURNING *;

-- name: GetModelRouting :one
SELECT * FROM model_routings
WHERE project_id = $1 AND environment = $2 AND logical_model = $3;

-- name: ListModelRoutings :many
SELECT * FROM model_routings
WHERE project_id = $1 AND environment = $2
ORDER BY logical_model ASC;

-- name: DeleteModelRouting :exec
DELETE FROM model_routings
WHERE project_id = $1 AND environment = $2 AND logical_model = $3;
