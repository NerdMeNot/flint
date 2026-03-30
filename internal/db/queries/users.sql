-- name: ListUsers :many
SELECT id, email, name, avatar_url, created_at
FROM users WHERE org_id = $1
ORDER BY name LIMIT 100;

-- name: SearchUsers :many
SELECT id, email, name, avatar_url, created_at
FROM users WHERE org_id = $1 AND (email ILIKE $2 OR name ILIKE $2)
ORDER BY name LIMIT 100;

-- name: UpsertUser :one
INSERT INTO users (org_id, email, external_id, name)
VALUES ($1, $2, $3, $4)
ON CONFLICT (org_id, external_id) DO UPDATE SET
    email = EXCLUDED.email, name = EXCLUDED.name
RETURNING id;

-- name: GetUserByEmail :one
SELECT id, email, external_id, name FROM users
WHERE org_id = $1 AND email = $2;
