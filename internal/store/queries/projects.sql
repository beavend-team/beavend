-- name: CreateProject :one
INSERT INTO projects (id, name, description, created_at, updated_at)
VALUES ($1, $2, $3, NOW(), NOW())
RETURNING *;

-- name: GetProject :one
SELECT * FROM projects
WHERE id = $1;

-- name: ListProjects :many
SELECT * FROM projects
ORDER BY created_at DESC;

-- name: UpdateProject :one
UPDATE projects
SET name = $2, description = $3, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: DeleteProject :exec
DELETE FROM projects
WHERE id = $1;

-- name: CreateEnvironment :one
INSERT INTO environments (project_id, name, description, created_at, updated_at)
VALUES ($1, $2, $3, NOW(), NOW())
RETURNING *;

-- name: GetEnvironment :one
SELECT * FROM environments
WHERE project_id = $1 AND name = $2;

-- name: ListEnvironments :many
SELECT * FROM environments
WHERE project_id = $1
ORDER BY created_at ASC;

-- name: DeleteEnvironment :exec
DELETE FROM environments
WHERE project_id = $1 AND name = $2;

-- name: GetProjectRevision :one
SELECT * FROM project_revisions
WHERE project_id = $1 AND environment = $2;

-- name: IncrementProjectRevision :one
INSERT INTO project_revisions (project_id, environment, revision, updated_at)
VALUES ($1, $2, 1, NOW())
ON CONFLICT (project_id, environment)
DO UPDATE SET revision = project_revisions.revision + 1, updated_at = NOW()
RETURNING revision;

-- name: UpsertProjectRevision :one
INSERT INTO project_revisions (project_id, environment, revision, updated_at)
VALUES ($1, $2, $3, NOW())
ON CONFLICT (project_id, environment)
DO UPDATE SET revision = EXCLUDED.revision, updated_at = NOW()
RETURNING *;
