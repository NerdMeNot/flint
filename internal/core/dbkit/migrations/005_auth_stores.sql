-- +goose Up
-- Move the device-authorization and MFA-pending stores from process-local memory
-- to Postgres so the auth layer is correct under concurrency (atomic claims, no
-- TOCTOU), survives restarts, and works across multiple server replicas. Also
-- add per-user TOTP replay tracking.

-- Device authorization flow (TUI/CLI login). access_token/refresh_token are set
-- on completion and live here only until the next poll claims (and deletes) the
-- row — a short-lived window bounded by expires_at. last_polled_at enforces the
-- OAuth device-flow polling interval (slow_down).
CREATE TABLE device_codes (
    device_code    text PRIMARY KEY,
    user_code      text NOT NULL,
    oauth_state    text,
    nonce          text,
    completed      boolean NOT NULL DEFAULT false,
    access_token   text,
    refresh_token  text,
    user_id        uuid REFERENCES users(id) ON DELETE CASCADE,
    interval_secs  int NOT NULL DEFAULT 5,
    last_polled_at timestamptz,
    expires_at     timestamptz NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_device_codes_user_code ON device_codes (user_code) WHERE NOT completed;
CREATE INDEX idx_device_codes_oauth_state ON device_codes (oauth_state) WHERE oauth_state IS NOT NULL;
CREATE INDEX idx_device_codes_expiry ON device_codes (expires_at);

-- Temporary token bridging password verification and the MFA second factor.
CREATE TABLE mfa_pending_tokens (
    token      text PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email      text NOT NULL,
    org_id     uuid NOT NULL REFERENCES orgs(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_mfa_pending_expiry ON mfa_pending_tokens (expires_at);

-- TOTP replay protection: the time-step (unix/30) of the last accepted code.
-- A code whose period is <= this value is rejected as a replay.
ALTER TABLE users ADD COLUMN mfa_last_used_period bigint;

-- +goose Down
ALTER TABLE users DROP COLUMN mfa_last_used_period;
DROP TABLE mfa_pending_tokens;
DROP TABLE device_codes;
