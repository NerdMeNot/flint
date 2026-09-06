-- name: ListPersonalTokensByUser :many
SELECT id, name, expires_at, last_used_at, created_at
FROM personal_tokens WHERE user_id = $1
ORDER BY created_at DESC;

-- name: CreatePersonalToken :one
INSERT INTO personal_tokens (user_id, name, token_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: DeletePersonalToken :exec
DELETE FROM personal_tokens WHERE id = $1;

-- name: GetPersonalTokenByHash :one
-- Authenticate a presented personal access token by the SHA-256 digest of its
-- raw value — one indexed lookup, not a bcrypt scan of every live token.
-- Deactivated owners are filtered here so a PAT dies with its user.
SELECT pt.id, pt.user_id, pt.expires_at,
       u.email, u.org_id, COALESCE(u.name, '')::text AS name
FROM personal_tokens pt JOIN users u ON u.id = pt.user_id
WHERE pt.token_hash = $1
  AND (pt.expires_at IS NULL OR pt.expires_at > now())
  AND u.is_active;

-- name: TouchPersonalToken :exec
UPDATE personal_tokens SET last_used_at = now() WHERE id = $1;
