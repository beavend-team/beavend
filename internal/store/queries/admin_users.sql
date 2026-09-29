-- name: CreateAdminUser :one
INSERT INTO admin_users (id, email, password_hash, role, created_at, updated_at)
VALUES ($1, $2, $3, $4, NOW(), NOW())
RETURNING *;

-- name: GetAdminUserByEmail :one
SELECT * FROM admin_users
WHERE email = $1;

-- name: GetAdminUserByID :one
SELECT * FROM admin_users
WHERE id = $1;

-- name: ListAdminUsers :many
SELECT * FROM admin_users
ORDER BY created_at ASC;

-- name: UpdateAdminUserPassword :one
UPDATE admin_users
SET password_hash = $2, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: UpdateAdminUserRole :one
UPDATE admin_users
SET role = $2, updated_at = NOW()
WHERE id = $1
RETURNING *;

-- name: DeleteAdminUser :exec
DELETE FROM admin_users
WHERE id = $1;

-- name: CountAdminUsers :one
SELECT COUNT(*) FROM admin_users;
