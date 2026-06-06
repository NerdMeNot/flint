-- name: ListEnvironments :many
SELECT id, name, slug, created_at
FROM environments WHERE org_id = $1
ORDER BY name;

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
ORDER BY name;

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
DO UPDATE SET value = $3, updated_at = now();

-- name: DeleteEnvVariableValue :exec
DELETE FROM env_variable_values
WHERE variable_id = $1 AND environment_id = $2;
