-- Device authorization flow (DB-backed; replaces the in-memory map).

-- name: InsertDeviceCode :exec
INSERT INTO device_codes (device_code, user_code, expires_at, interval_secs)
VALUES ($1, $2, $3, $4);

-- name: GetDeviceCode :one
SELECT completed, expires_at, last_polled_at, interval_secs
FROM device_codes WHERE device_code = $1;

-- name: TouchDeviceCodePoll :exec
UPDATE device_codes SET last_polled_at = now() WHERE device_code = $1;

-- name: SetDeviceCodeOAuthState :exec
UPDATE device_codes SET oauth_state = $2, nonce = $3, code_verifier = $4 WHERE device_code = $1;

-- name: CompleteDeviceCode :exec
UPDATE device_codes
SET completed = true, access_token = $2, refresh_token = $3, user_id = $4
WHERE device_code = $1;

-- name: ClaimCompletedDeviceCode :one
-- Atomic claim: return the tokens and delete the row in one statement, only if
-- completed. This is the TOCTOU fix — no read-then-mutate window.
DELETE FROM device_codes WHERE device_code = $1 AND completed = true
RETURNING access_token, refresh_token;

-- name: DeleteDeviceCode :exec
DELETE FROM device_codes WHERE device_code = $1;

-- name: GetDeviceCodeNonce :one
SELECT nonce FROM device_codes WHERE device_code = $1;

-- name: GetDeviceCodeCodeVerifier :one
SELECT code_verifier FROM device_codes WHERE device_code = $1;

-- name: GetDeviceCodeRefreshToken :one
SELECT refresh_token FROM device_codes WHERE device_code = $1;

-- name: FindDeviceCodeByUserCode :one
SELECT device_code FROM device_codes
WHERE user_code = $1 AND NOT completed AND expires_at > now()
LIMIT 1;

-- name: FindDeviceCodeByOAuthState :one
SELECT device_code FROM device_codes
WHERE oauth_state = $1 AND expires_at > now()
LIMIT 1;

-- name: DeleteExpiredDeviceCodes :exec
DELETE FROM device_codes WHERE expires_at < now();

-- MFA pending tokens (DB-backed; replaces the in-memory map).

-- name: InsertMFAPendingToken :exec
INSERT INTO mfa_pending_tokens (token, user_id, email, org_id, expires_at)
VALUES ($1, $2, $3, $4, $5);

-- name: GetMFAPendingToken :one
SELECT user_id, email, org_id FROM mfa_pending_tokens
WHERE token = $1 AND expires_at > now();

-- name: DeleteMFAPendingToken :exec
DELETE FROM mfa_pending_tokens WHERE token = $1;

-- name: DeleteExpiredMFAPendingTokens :exec
DELETE FROM mfa_pending_tokens WHERE expires_at < now();

-- TOTP replay protection.

-- name: RecordTOTPUse :one
-- Advance the user's last-used TOTP period atomically. Returns a row only when
-- the new period is strictly greater than the stored one; no row means the code
-- was already used (replay) and must be rejected.
UPDATE users SET mfa_last_used_period = $2
WHERE id = $1 AND (mfa_last_used_period IS NULL OR mfa_last_used_period < $2)
RETURNING id;
