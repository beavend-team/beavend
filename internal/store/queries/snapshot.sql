-- name: GetSnapshotRevision :one
SELECT revision FROM project_revisions
WHERE project_id = $1 AND environment = $2;

-- name: GetSnapshotEnvironment :one
SELECT auth_config FROM environments
WHERE project_id = $1 AND name = $2;

-- name: GetSnapshotApps :many
SELECT aav.*, a.name, a.description
FROM ai_app_versions aav
JOIN ai_apps a ON a.id = aav.app_id AND a.active_version = aav.version
WHERE a.project_id = $1 AND a.environment = $2;

-- name: GetSnapshotPrompts :many
SELECT pv.*, p.name AS prompt_name
FROM prompt_versions pv
JOIN prompts p ON p.id = pv.prompt_id AND p.active_version = pv.version
WHERE p.project_id = $1 AND p.environment = $2;

-- name: GetSnapshotPolicies :many
SELECT pv.*, p.name, p.description
FROM policy_versions pv
JOIN policies p ON p.id = pv.policy_id AND p.active_version = pv.version
WHERE p.project_id = $1 AND p.environment = $2;

-- name: GetSnapshotTools :many
SELECT tv.*, t.name, t.description
FROM tool_versions tv
JOIN tools t ON t.id = tv.tool_id AND t.active_version = tv.version
WHERE t.project_id = $1 AND t.environment = $2;

-- name: GetSnapshotModelRoutings :many
SELECT * FROM model_routings
WHERE project_id = $1 AND environment = $2;

-- name: ListAllProjectEnvironments :many
SELECT e.project_id, e.name AS environment, COALESCE(pr.revision, 1)::BIGINT AS revision
FROM environments e
LEFT JOIN project_revisions pr ON pr.project_id = e.project_id AND pr.environment = e.name
ORDER BY e.project_id, e.name;
