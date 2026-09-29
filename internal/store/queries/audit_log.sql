-- name: CreateAuditLogEntry :one
INSERT INTO audit_log (
    project_id, environment, user_id, action,
    resource_type, resource_id, payload, created_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
RETURNING *;

-- name: ListAuditLogsByProjectEnv :many
SELECT * FROM audit_log
WHERE project_id = $1 AND environment = $2
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

-- name: ListAuditLogs :many
SELECT * FROM audit_log
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountAuditLogsByProjectEnv :one
SELECT COUNT(*) FROM audit_log
WHERE project_id = $1 AND environment = $2;

-- name: CountAuditLogs :one
SELECT COUNT(*) FROM audit_log;
