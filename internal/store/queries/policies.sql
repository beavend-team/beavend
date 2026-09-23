-- name: CreatePolicy :one
INSERT INTO policies (id, project_id, environment, name, description, active_version, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
RETURNING *;

-- name: GetPolicy :one
SELECT * FROM policies
WHERE id = $1;

-- name: ListPolicies :many
SELECT * FROM policies
WHERE project_id = $1 AND environment = $2
ORDER BY name ASC;

-- name: UpdatePolicyActiveVersion :one
UPDATE policies
SET active_version = $2, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: CreatePolicyVersion :one
INSERT INTO policy_versions (id, policy_id, project_id, environment, version, expression, is_active, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
RETURNING *;

-- name: GetPolicyVersion :one
SELECT * FROM policy_versions
WHERE policy_id = $1 AND version = $2;

-- name: GetActivePolicyVersion :one
SELECT pv.*
FROM policy_versions pv
JOIN policies p ON p.id = pv.policy_id AND p.active_version = pv.version
WHERE p.id = $1;

-- name: ListActivePolicyVersions :many
SELECT pv.*
FROM policy_versions pv
JOIN policies p ON p.id = pv.policy_id AND p.active_version = pv.version
WHERE p.project_id = $1 AND p.environment = $2;
