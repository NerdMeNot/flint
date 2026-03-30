-- name: ListAPIKeys :many
SELECT id, name, scopes, expires_at, last_used_at, created_at
FROM api_keys WHERE org_id = $1 ORDER BY created_at;

-- name: CreateAPIKey :one
INSERT INTO api_keys (org_id, user_id, name, key_hash, scopes)
VALUES ($1, $2, $3, $4, $5) RETURNING id;

-- name: DeleteAPIKey :execrows
DELETE FROM api_keys WHERE id = $1;

-- name: ListValidAPIKeys :many
SELECT id, org_id, user_id, name, key_hash, scopes
FROM api_keys WHERE (expires_at IS NULL OR expires_at > now());

-- name: TouchAPIKey :exec
UPDATE api_keys SET last_used_at = now() WHERE id = $1;
