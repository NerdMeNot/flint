-- name: CreateSession :one
INSERT INTO sessions (user_id, token_hash, idp_token_enc, ip_address, user_agent,
    expires_at, idle_expires_at)
VALUES ($1, $2, $3, $4::inet, $5,
    now() + make_interval(secs := sqlc.arg(abs_lifetime_secs)),
    now() + make_interval(secs := sqlc.arg(idle_lifetime_secs)))
RETURNING id;

-- name: GetSessionByTokenHash :one
SELECT id, user_id, token_hash, idp_token_enc, ip_address, user_agent,
       created_at, last_activity, expires_at, idle_expires_at, revoked_at
FROM sessions
WHERE token_hash = $1;

-- name: RotateSessionToken :exec
UPDATE sessions
SET token_hash = sqlc.arg(new_hash),
    last_activity = now(),
    idle_expires_at = now() + make_interval(secs := sqlc.arg(idle_lifetime_secs))
WHERE id = $1 AND revoked_at IS NULL;

-- name: UpdateSessionIdpToken :exec
UPDATE sessions SET idp_token_enc = $2 WHERE id = $1;

-- name: RevokeSession :exec
UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeSessionByHash :exec
UPDATE sessions SET revoked_at = now() WHERE token_hash = $1 AND revoked_at IS NULL;

-- name: RevokeUserSessions :exec
UPDATE sessions SET revoked_at = now()
WHERE user_id = $1 AND revoked_at IS NULL;

-- name: ListUserSessions :many
SELECT id, ip_address, user_agent, created_at, last_activity, expires_at
FROM sessions
WHERE user_id = $1 AND revoked_at IS NULL
  AND expires_at > now() AND idle_expires_at > now()
ORDER BY last_activity DESC;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions
WHERE (revoked_at IS NOT NULL AND revoked_at < now() - interval '7 days')
   OR (expires_at < now() - interval '7 days');

-- name: ListSessionsForSync :many
SELECT s.id, s.user_id, s.idp_token_enc, s.last_synced_at,
       u.email, u.org_id
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.revoked_at IS NULL
  AND s.expires_at > now()
  AND s.idle_expires_at > now()
  AND s.idp_token_enc IS NOT NULL
  AND (s.last_synced_at IS NULL OR s.last_synced_at < now() - make_interval(secs := $1))
ORDER BY s.last_synced_at NULLS FIRST
LIMIT $2;

-- name: UpdateSessionSyncedAt :exec
UPDATE sessions SET last_synced_at = now() WHERE id = $1;
