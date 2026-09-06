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
    saml_request_id text, -- AuthnRequest ID, bound to the SAML response (InResponseTo)

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
    -- Failed second-factor attempts against this token. A 6-digit code is a
    -- 10^6 space, which is only a second factor if guessing is bounded; the
    -- endpoint had no limit of any kind, and a fresh token cost nothing but a
    -- successful password login (which does not count as a failure).
    attempts   int NOT NULL DEFAULT 0,
    -- 'verify'  — password accepted, awaiting the second factor.
    -- 'enrol'   — password accepted but the user's role requires MFA they have
    --             not set up yet. Grants nothing except TOTP enrolment; without
    --             it, requiring MFA locked the user out, because enrolling
    --             needs a session and login refuses to issue one.
    purpose    text NOT NULL DEFAULT 'verify',
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_mfa_pending_expiry ON public.mfa_pending_tokens (expires_at);

-- One-time-use guard for SAML assertions: an assertion ID may be consumed once,
-- within its validity window, defeating replay of a captured response.
CREATE TABLE public.saml_used_assertions (
    assertion_id text PRIMARY KEY,
    expires_at   timestamptz NOT NULL
);
CREATE INDEX idx_saml_used_assertions_expiry ON public.saml_used_assertions (expires_at);

-- +goose Down
DROP TABLE public.saml_used_assertions;
DROP TABLE public.mfa_pending_tokens;
DROP TABLE public.device_codes;
