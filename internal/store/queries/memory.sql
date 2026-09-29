-- name: CreateSemanticMemory :one
INSERT INTO semantic_memories (
    id, project_id, environment, ai_app_id, issuer, subject, content, embedding, metadata, created_at, updated_at, expires_at
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW(), $10
)
RETURNING *;

-- name: RecallSemanticMemories :many
SELECT * FROM semantic_memories
WHERE project_id = $1
  AND environment = $2
  AND issuer = $3
  AND subject = $4
  AND (ai_app_id = $5 OR $5 = '')
  AND (expires_at IS NULL OR expires_at > NOW())
ORDER BY created_at DESC
LIMIT $6;

-- name: SearchSemanticMemories :many
SELECT id, project_id, environment, ai_app_id, issuer, subject, content, embedding, metadata, created_at, updated_at, expires_at,
       (1.0 - (embedding <=> $6))::float8 AS similarity
FROM semantic_memories
WHERE project_id = $1
  AND environment = $2
  AND issuer = $3
  AND subject = $4
  AND (ai_app_id = $5 OR $5 = '')
  AND (expires_at IS NULL OR expires_at > NOW())
ORDER BY embedding <=> $6
LIMIT $7;

-- name: DeleteSemanticMemoriesBySubject :exec
DELETE FROM semantic_memories
WHERE project_id = $1 AND issuer = $2 AND subject = $3;

-- name: DeleteExpiredSemanticMemories :exec
DELETE FROM semantic_memories
WHERE expires_at IS NOT NULL AND expires_at <= NOW();
