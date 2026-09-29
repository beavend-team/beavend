-- name: CreateConversationThread :one
INSERT INTO conversation_threads (
    id, project_id, environment, ai_app_id, issuer, subject, messages, metadata, ttl_seconds, created_at, updated_at, expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW(), $10
)
RETURNING *;

-- name: GetConversationThread :one
SELECT * FROM conversation_threads
WHERE project_id = $1 AND environment = $2 AND id = $3 AND expires_at > NOW();

-- name: UpdateConversationThreadMessages :one
UPDATE conversation_threads
SET messages = $4, metadata = $5, updated_at = NOW(), expires_at = $6
WHERE project_id = $1 AND environment = $2 AND id = $3
RETURNING *;

-- name: DeleteConversationThread :exec
DELETE FROM conversation_threads
WHERE project_id = $1 AND environment = $2 AND id = $3;

-- name: DeleteConversationThreadsBySubject :exec
DELETE FROM conversation_threads
WHERE project_id = $1 AND environment = $2 AND issuer = $3 AND subject = $4;

-- name: DeleteExpiredConversationThreads :exec
DELETE FROM conversation_threads
WHERE expires_at <= NOW();
