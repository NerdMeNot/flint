-- +goose Up

-- ────────────────────────────────────────────────────────────
-- Server-side sessions with refresh token rotation
-- ────────────────────────────────────────────────────────────
CREATE TABLE sessions (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash      text NOT NULL UNIQUE,
    idp_token_enc   bytea,
    ip_address      inet,
    user_agent      text,
    created_at      timestamptz NOT NULL DEFAULT now(),
    last_activity   timestamptz NOT NULL DEFAULT now(),
    last_synced_at  timestamptz,
    expires_at      timestamptz NOT NULL,
    idle_expires_at timestamptz NOT NULL,
    revoked_at      timestamptz
);

CREATE INDEX idx_sessions_user ON sessions (user_id) WHERE revoked_at IS NULL;
CREATE INDEX idx_sessions_expiry ON sessions (expires_at) WHERE revoked_at IS NULL;
CREATE INDEX idx_sessions_sync ON sessions (last_synced_at)
    WHERE revoked_at IS NULL AND idp_token_enc IS NOT NULL;

-- ────────────────────────────────────────────────────────────
-- Add is_active flag to users for deprovisioning
-- ────────────────────────────────────────────────────────────
ALTER TABLE users ADD COLUMN IF NOT EXISTS is_active boolean NOT NULL DEFAULT true;


-- +goose Down

ALTER TABLE users DROP COLUMN IF EXISTS is_active;
DROP TABLE IF EXISTS sessions;
