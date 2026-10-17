-- name: CreateSkill :one
INSERT INTO skills (id, project_id, environment, name, description, source, injection_mode, active_version, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
RETURNING *;

-- name: GetSkill :one
SELECT * FROM skills
WHERE id = $1;

-- name: ListSkills :many
SELECT * FROM skills
WHERE project_id = $1 AND environment = $2
ORDER BY name ASC;

-- name: UpdateSkillActiveVersion :one
UPDATE skills
SET active_version = $2, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateSkill :one
UPDATE skills
SET name = $2, description = $3, source = $4, injection_mode = $5, active_version = $6, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: CreateSkillVersion :one
INSERT INTO skill_versions (id, skill_id, project_id, environment, version, content, variables, is_active, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
RETURNING *;

-- name: GetSkillVersion :one
SELECT * FROM skill_versions
WHERE skill_id = $1 AND version = $2;

-- name: GetActiveSkillVersion :one
SELECT sv.*
FROM skill_versions sv
JOIN skills s ON s.id = sv.skill_id AND s.active_version = sv.version
WHERE s.id = $1;

-- name: ListActiveSkillVersions :many
SELECT sv.*
FROM skill_versions sv
JOIN skills s ON s.id = sv.skill_id AND s.active_version = sv.version
WHERE s.project_id = $1 AND s.environment = $2;

-- name: ListSkillVersions :many
SELECT * FROM skill_versions
WHERE skill_id = $1
ORDER BY version DESC;

-- name: DeleteSkill :exec
DELETE FROM skills
WHERE id = $1;
