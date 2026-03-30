-- +goose Up

-- ────────────────────────────────────────────────────────────
-- Auth provider config: stores OIDC/SAML configuration
-- written by the AuthProvider CRD controller.
-- Server reads this at boot, falls back to config.yaml.
-- ────────────────────────────────────────────────────────────
CREATE TABLE auth_provider_config (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_type   text NOT NULL CHECK (provider_type IN ('oidc', 'saml')),
    display_name    text NOT NULL DEFAULT 'default',
    config_enc      bytea NOT NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (provider_type)
);

-- +goose Down
DROP TABLE IF EXISTS auth_provider_config;
