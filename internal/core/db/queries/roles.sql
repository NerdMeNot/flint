-- name: ListRoles :many
SELECT id, name, slug, description, is_system, require_mfa, created_at
FROM roles WHERE org_id = $1 ORDER BY is_system DESC, name
LIMIT $2 OFFSET $3;

-- name: GetRoleBySlug :one
SELECT id, name, slug, description, is_system, created_at
FROM roles WHERE org_id = $1 AND slug = $2;

-- name: GetRoleByID :one
SELECT id, name, slug, description, is_system, require_mfa, created_at
FROM roles WHERE id = $1;

-- name: UpdateRole :execrows
UPDATE roles
SET name = COALESCE(sqlc.narg('name'), name),
    description = COALESCE(sqlc.narg('description'), description),
    require_mfa = COALESCE(sqlc.narg('require_mfa'), require_mfa)
WHERE id = sqlc.arg('id') AND is_system = false;

-- name: CreateRole :one
INSERT INTO roles (org_id, name, slug, description, is_system, require_mfa)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING id;

-- name: DeleteRole :execrows
DELETE FROM roles WHERE id = $1 AND is_system = false;

-- name: RoleExists :one
SELECT EXISTS(SELECT 1 FROM roles WHERE org_id = $1 AND slug = $2) AS role_exists;
