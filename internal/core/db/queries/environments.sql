-- name: ListEnvironments :many
SELECT id, name, slug, created_at
FROM environments WHERE org_id = $1
ORDER BY name
LIMIT $2 OFFSET $3;

-- name: GetEnvironment :one
SELECT id, name, slug, created_at
FROM environments WHERE id = $1;

-- name: CreateEnvironment :one
INSERT INTO environments (org_id, name, slug)
VALUES ($1, $2, $3)
RETURNING id;

-- name: DeleteEnvironment :exec
DELETE FROM environments WHERE id = $1;

-- name: ListEnvVariables :many
SELECT id, name, description, scope, is_secret, created_at
FROM env_variables WHERE org_id = $1
ORDER BY name
LIMIT $2 OFFSET $3;

-- name: GetEnvVariable :one
SELECT id, name, description, scope, is_secret, created_at
FROM env_variables WHERE id = $1;

-- name: CreateEnvVariable :one
INSERT INTO env_variables (org_id, name, description, scope, is_secret)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: DeleteEnvVariable :exec
DELETE FROM env_variables WHERE id = $1;

-- name: IsEnvVariableSecret :one
SELECT is_secret FROM env_variables WHERE id = $1;

-- name: ListEnvVariableValues :many
SELECT evv.variable_id, evv.environment_id::text AS environment_id,
       evv.value, evv.updated_at, ev.is_secret
FROM env_variable_values evv
JOIN env_variables ev ON ev.id = evv.variable_id
WHERE ev.org_id = $1
ORDER BY ev.name, evv.environment_id;

-- name: GetGlobalVariableValue :one
SELECT value FROM env_variable_values
WHERE variable_id = $1 AND environment_id IS NULL;

-- name: GlobalVariableValueExists :one
SELECT EXISTS(
    SELECT 1 FROM env_variable_values
    WHERE variable_id = $1 AND environment_id IS NULL
);

-- name: UpsertEnvVariableValue :exec
INSERT INTO env_variable_values (variable_id, environment_id, value, updated_at)
VALUES ($1, $2, $3, now())
ON CONFLICT (variable_id, environment_id)
DO UPDATE SET value = $3, value_enc = NULL, updated_at = now();

-- name: UpsertSecretEnvVariableValue :exec
-- Stores an encrypted secret value. `value` is kept empty; the ciphertext lives
-- in `value_enc`.
INSERT INTO env_variable_values (variable_id, environment_id, value, value_enc, updated_at)
VALUES ($1, $2, '', $3, now())
ON CONFLICT (variable_id, environment_id)
DO UPDATE SET value = '', value_enc = $3, updated_at = now();

-- name: GetSecretEnvVarValue :one
-- Returns the encrypted value of a secret env-var by org + name, scoped to an
-- environment slug (the empty string selects the global value). Used by the
-- agent secret-injection path; decrypted server-side before being returned.
SELECT evv.value_enc
FROM env_variables ev
JOIN env_variable_values evv ON evv.variable_id = ev.id
LEFT JOIN environments e ON e.id = evv.environment_id
WHERE ev.org_id = sqlc.arg('org_id')
  AND ev.name = sqlc.arg('name')
  AND ev.is_secret = true
  AND ( (sqlc.arg('env_slug')::text = '' AND evv.environment_id IS NULL)
        OR e.slug = sqlc.arg('env_slug')::text )
LIMIT 1;

-- name: DeleteEnvVariableValue :exec
DELETE FROM env_variable_values
WHERE variable_id = $1 AND environment_id = $2;
