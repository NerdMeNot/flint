-- name: GetAuthProviderConfig :one
SELECT id, provider_type, display_name, config_enc, created_at, updated_at
FROM auth_provider_config WHERE provider_type = $1;

-- name: UpsertAuthProviderConfig :one
INSERT INTO auth_provider_config (provider_type, display_name, config_enc)
VALUES ($1, $2, $3)
ON CONFLICT (provider_type) DO UPDATE SET
    display_name = EXCLUDED.display_name,
    config_enc = EXCLUDED.config_enc,
    updated_at = now()
RETURNING id;

-- name: DeleteAuthProviderConfig :execrows
DELETE FROM auth_provider_config WHERE provider_type = $1;

-- name: ListAuthProviderConfigNames :many
SELECT id, display_name, provider_type FROM auth_provider_config;
