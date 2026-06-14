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

-- name: ListValidPersonalTokensWithUser :many
-- All non-expired personal tokens with their owner, for bearer-token auth
-- (the caller bcrypt-compares each hash).
SELECT pt.id, pt.user_id, pt.token_hash, pt.expires_at,
       u.email, u.org_id, COALESCE(u.name, '')::text AS name
FROM personal_tokens pt JOIN users u ON u.id = pt.user_id
WHERE pt.expires_at IS NULL OR pt.expires_at > now();

-- name: TouchPersonalToken :exec
UPDATE personal_tokens SET last_used_at = now() WHERE id = $1;
