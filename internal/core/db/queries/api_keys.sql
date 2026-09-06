-- name: ListAPIKeys :many
SELECT id, name, scopes, expires_at, last_used_at, created_at
FROM api_keys WHERE org_id = $1 ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: CreateAPIKey :one
INSERT INTO api_keys (org_id, user_id, name, key_hash, scopes)
VALUES ($1, $2, $3, $4, $5) RETURNING id;

-- name: DeleteAPIKey :execrows
DELETE FROM api_keys WHERE id = $1;

-- name: GetAPIKeyByHash :one
-- Authenticate a presented API key by the SHA-256 digest of its raw value.
-- API keys are high-entropy random strings, so a digest plus the unique index
-- is the right primitive: one indexed lookup instead of bcrypt-comparing every
-- key in the table on every request.
SELECT id, org_id, user_id, name, scopes
FROM api_keys
WHERE key_hash = $1 AND (expires_at IS NULL OR expires_at > now());

-- name: TouchAPIKey :exec
UPDATE api_keys SET last_used_at = now() WHERE id = $1;

-- name: ListAPIKeysDetailed :many
-- API keys with role slug + creator email for the settings list.
SELECT ak.id, ak.name, COALESCE(r.slug, 'viewer')::text AS role,
       COALESCE(u.email, 'system')::text AS created_by,
       ak.expires_at, ak.last_used_at, ak.created_at
FROM api_keys ak
LEFT JOIN roles r ON r.id = ak.role_id
LEFT JOIN users u ON u.id = ak.user_id
WHERE ak.org_id = $1 ORDER BY ak.created_at DESC LIMIT $2 OFFSET $3;
