-- +goose Up

-- ────────────────────────────────────────────────────────────
-- Local auth: password + TOTP MFA columns on users
-- ────────────────────────────────────────────────────────────
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_hash text;
ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_secret_enc bytea;
ALTER TABLE users ADD COLUMN IF NOT EXISTS totp_verified boolean NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS mfa_required_override boolean;
ALTER TABLE users ADD COLUMN IF NOT EXISTS password_changed_at timestamptz;
ALTER TABLE users ADD COLUMN IF NOT EXISTS recovery_codes text[];

-- ────────────────────────────────────────────────────────────
-- Login attempt tracking for brute-force rate limiting
-- ────────────────────────────────────────────────────────────
CREATE TABLE login_attempts (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email      text NOT NULL,
    ip_address inet,
    success    boolean NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_login_attempts_email ON login_attempts (email, created_at DESC);
CREATE INDEX idx_login_attempts_cleanup ON login_attempts (created_at);

-- ────────────────────────────────────────────────────────────
-- Per-role MFA requirement
-- ────────────────────────────────────────────────────────────
ALTER TABLE roles ADD COLUMN IF NOT EXISTS require_mfa boolean NOT NULL DEFAULT false;

-- Admin role requires MFA by default.
UPDATE roles SET require_mfa = true WHERE slug = 'admin';


-- +goose Down

UPDATE roles SET require_mfa = false WHERE slug = 'admin';
ALTER TABLE roles DROP COLUMN IF EXISTS require_mfa;
DROP TABLE IF EXISTS login_attempts;
ALTER TABLE users DROP COLUMN IF EXISTS recovery_codes;
ALTER TABLE users DROP COLUMN IF EXISTS password_changed_at;
ALTER TABLE users DROP COLUMN IF EXISTS mfa_required_override;
ALTER TABLE users DROP COLUMN IF EXISTS totp_verified;
ALTER TABLE users DROP COLUMN IF EXISTS totp_secret_enc;
ALTER TABLE users DROP COLUMN IF EXISTS password_hash;
