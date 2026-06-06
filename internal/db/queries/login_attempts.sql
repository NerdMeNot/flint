-- name: RecordLoginAttempt :exec
INSERT INTO login_attempts (email, ip_address, success)
VALUES (sqlc.arg(email), sqlc.arg(ip_address)::inet, sqlc.arg(success));

-- name: CountRecentFailuresByEmail :one
SELECT COUNT(*) FROM login_attempts
WHERE email = $1 AND success = false
  AND created_at > now() - interval '15 minutes';

-- name: CountRecentFailuresByIP :one
SELECT COUNT(*) FROM login_attempts
WHERE ip_address = sqlc.arg(ip_address)::inet AND success = false
  AND created_at > now() - interval '15 minutes';

-- name: CleanupOldLoginAttempts :exec
DELETE FROM login_attempts WHERE created_at < now() - interval '24 hours';
