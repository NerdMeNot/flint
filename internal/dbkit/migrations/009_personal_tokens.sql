-- +goose Up

-- ────────────────────────────────────────────────────────────
-- Personal tokens: user-scoped API tokens for CLI/TUI auth.
-- Unlike admin API keys, personal tokens inherit the user's
-- own role assignments — no separate role or scope needed.
-- ────────────────────────────────────────────────────────────
CREATE TABLE personal_tokens (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         text NOT NULL,
    token_hash   text NOT NULL UNIQUE,
    expires_at   timestamptz,
    last_used_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_personal_tokens_user ON personal_tokens(user_id);
CREATE INDEX idx_personal_tokens_hash ON personal_tokens(token_hash);

-- +goose Down
DROP TABLE IF EXISTS personal_tokens;
