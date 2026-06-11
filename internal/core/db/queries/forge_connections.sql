-- name: ListForgeConnections :many
SELECT id, forge_type, display_name, app_id, installation_id, created_at
FROM forge_connections WHERE org_id = $1 ORDER BY created_at DESC
LIMIT $2 OFFSET $3;

-- name: GetWebhookSecret :one
SELECT webhook_secret FROM forge_connections WHERE forge_type = $1 LIMIT 1;

-- name: GetWebhookSecretByName :one
SELECT webhook_secret FROM forge_connections WHERE display_name = $1 LIMIT 1;

-- name: DeleteForgeConnection :execrows
DELETE FROM forge_connections WHERE id = $1;

-- name: GetCloneCredentials :one
SELECT fc.forge_type, fc.credentials_enc
FROM projects p
JOIN forge_connections fc ON p.forge_id = fc.id
WHERE p.repo_path = $1 AND p.is_archived = false
LIMIT 1;

-- name: ListForgeConnectionNames :many
SELECT id, display_name FROM forge_connections;

-- name: InsertForgeConnection :one
INSERT INTO forge_connections (org_id, forge_type, display_name, webhook_secret, credentials_enc)
VALUES (
    (SELECT id FROM orgs LIMIT 1),
    $1, $2, $3, $4
)
RETURNING id;

-- name: UpdateForgeConnectionByName :one
UPDATE forge_connections
SET forge_type = $1, display_name = $2, webhook_secret = $3, credentials_enc = $4
WHERE display_name = $5
RETURNING id;

-- name: DeleteForgeConnectionByID :exec
DELETE FROM forge_connections WHERE id = $1;
