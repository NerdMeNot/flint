-- name: ListUsers :many
SELECT id, email, name, avatar_url, created_at
FROM users WHERE org_id = $1
ORDER BY name
LIMIT $2 OFFSET $3;

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
SELECT id, email, external_id, name, avatar_url, theme_mode, color_theme, totp_verified FROM users
WHERE org_id = $1 AND email = $2;

-- name: UpdateUserProfile :exec
-- Self-service profile update. NULL args leave the existing value untouched
-- (COALESCE), so callers can patch any subset of {name, avatar, appearance}.
UPDATE users SET
    name = COALESCE(sqlc.narg('name'), name),
    avatar_url = COALESCE(sqlc.narg('avatar_url'), avatar_url),
    theme_mode = COALESCE(sqlc.narg('theme_mode'), theme_mode),
    color_theme = COALESCE(sqlc.narg('color_theme'), color_theme)
WHERE id = sqlc.arg('id');

-- name: GetUserByID :one
SELECT id, email, external_id, name, is_active FROM users
WHERE id = $1;

-- name: GetUserForAuth :one
SELECT id, org_id, email, external_id, name, password_hash,
       totp_secret_enc, totp_verified, mfa_required_override, is_active,
       force_password_change
FROM users
WHERE org_id = $1 AND email = $2;

-- name: CreateLocalUser :one
INSERT INTO users (org_id, email, external_id, name, password_hash, password_changed_at)
VALUES ($1, $2, $2, $3, $4, now())
RETURNING id;

-- name: UpdateUserPassword :exec
UPDATE users SET password_hash = $2, password_changed_at = now()
WHERE id = $1;

-- name: SetUserTOTPSecret :exec
UPDATE users SET totp_secret_enc = $2 WHERE id = $1;

-- name: VerifyUserTOTP :exec
UPDATE users SET totp_verified = true WHERE id = $1;

-- name: ClearUserTOTP :exec
UPDATE users SET totp_secret_enc = NULL, totp_verified = false, recovery_codes = NULL
WHERE id = $1;

-- name: SetUserRecoveryCodes :exec
UPDATE users SET recovery_codes = $2 WHERE id = $1;

-- name: GetUserRecoveryCodes :one
SELECT recovery_codes FROM users WHERE id = $1;

-- name: CheckMFARequiredForUser :one
-- Does any role this subject holds demand a second factor?
--
-- Local (password) sign-in only. An SSO session is established by the IdP,
-- which owns the second factor there; Flint never sees whether one was
-- presented, so it does not pretend to enforce it. Requiring MFA for SSO users
-- is a setting on the IdP, not here.
SELECT EXISTS(
    SELECT 1 FROM role_assignments ra
    JOIN roles r ON r.id = ra.role_id
    WHERE ra.subject = $1 AND r.require_mfa = true
) AS required;

-- name: CountUsers :one
SELECT COUNT(*) FROM users WHERE org_id = $1;

-- name: ClearForcePasswordChange :exec
UPDATE users SET force_password_change = false WHERE id = $1;
