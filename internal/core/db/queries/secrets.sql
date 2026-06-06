-- name: GetSecret :one
SELECT encrypted_value FROM secrets
WHERE org_id = $1 AND COALESCE(project_id::text, '') = $2 AND name = $3;

-- name: UpsertSecret :exec
INSERT INTO secrets (org_id, project_id, name, encrypted_value, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (org_id, project_id, name)
DO UPDATE SET encrypted_value = $4, updated_at = $5;

-- name: DeleteSecret :execrows
DELETE FROM secrets
WHERE org_id = $1 AND COALESCE(project_id::text, '') = $2 AND name = $3;

-- name: ListSecrets :many
SELECT name, created_at, updated_at FROM secrets
WHERE org_id = $1 AND COALESCE(project_id::text, '') = $2
ORDER BY name;

-- name: ListProjectSecrets :many
SELECT name, created_at, updated_at FROM secrets
WHERE project_id = $1 ORDER BY name;

-- name: ListOrgSecrets :many
SELECT name, created_at, updated_at FROM secrets
WHERE org_id = $1 AND project_id IS NULL ORDER BY name;

-- name: ListAllSecrets :many
SELECT id, encrypted_value FROM secrets;

-- name: UpdateSecretValue :exec
UPDATE secrets SET encrypted_value = $2, updated_at = $3 WHERE id = $1;

-- name: GetSecretByEnvironment :one
SELECT id, encrypted_value FROM secrets
WHERE org_id = $1 AND project_id = $2 AND environment = $3 AND name = $4;
