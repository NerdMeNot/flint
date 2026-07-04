-- name: ListComputeProviders :many
SELECT * FROM compute_providers ORDER BY name;

-- name: GetComputeProvider :one
SELECT * FROM compute_providers WHERE name = $1;

-- name: UpsertComputeProvider :one
-- Also the config-file bootstrap path: a providers: block in config upserts by
-- name at startup so IaC/first-boot installs stay one-file; DB is source of truth.
INSERT INTO compute_providers (name, provider_type, config, credentials_enc)
VALUES (@name, @provider_type, @config, @credentials_enc)
ON CONFLICT (name) DO UPDATE SET
    provider_type = EXCLUDED.provider_type,
    config = EXCLUDED.config,
    credentials_enc = COALESCE(EXCLUDED.credentials_enc, compute_providers.credentials_enc),
    updated_at = now()
RETURNING id;

-- name: DeleteComputeProvider :exec
DELETE FROM compute_providers WHERE id = $1;
