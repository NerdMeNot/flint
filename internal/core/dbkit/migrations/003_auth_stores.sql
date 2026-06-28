-- +goose Up
-- Durable auth state (device-flow + MFA-pending) — Postgres-backed so it is
-- correct under concurrency, survives restarts, and works across replicas.

-- Device authorization flow (TUI/CLI login).
CREATE TABLE public.device_codes (
    device_code    text PRIMARY KEY,
    user_code      text NOT NULL,
    oauth_state    text,
    nonce          text,
    code_verifier  text, -- PKCE (S256) verifier for the OIDC code exchange

    completed      boolean NOT NULL DEFAULT false,
    access_token   text,
    refresh_token  text,
    user_id        uuid REFERENCES public.users(id) ON DELETE CASCADE,
    interval_secs  int NOT NULL DEFAULT 5,
    last_polled_at timestamptz,
    expires_at     timestamptz NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_device_codes_user_code ON public.device_codes (user_code) WHERE NOT completed;
CREATE INDEX idx_device_codes_oauth_state ON public.device_codes (oauth_state) WHERE oauth_state IS NOT NULL;
CREATE INDEX idx_device_codes_expiry ON public.device_codes (expires_at);

-- Temporary token bridging password verification and the MFA second factor.
CREATE TABLE public.mfa_pending_tokens (
    token      text PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    email      text NOT NULL,
    org_id     uuid NOT NULL REFERENCES public.orgs(id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_mfa_pending_expiry ON public.mfa_pending_tokens (expires_at);

-- +goose Down
DROP TABLE public.mfa_pending_tokens;
DROP TABLE public.device_codes;
