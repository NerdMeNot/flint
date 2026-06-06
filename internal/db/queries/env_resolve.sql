-- name: ResolveEnvVars :many
-- Returns resolved env variable name/value pairs for a given org.
-- For global vars: returns the single value (environment_id IS NULL).
-- For environment-scoped vars: returns the value matching the env slug.
-- Excludes is_secret=true (those go through the agent secrets flow).
SELECT v.name, val.value
FROM env_variables v
JOIN env_variable_values val ON val.variable_id = v.id
LEFT JOIN environments e ON e.id = val.environment_id
WHERE v.org_id = $1
  AND v.is_secret = false
  AND (
    (v.scope = 'global' AND val.environment_id IS NULL)
    OR
    (v.scope = 'environment' AND e.slug = sqlc.arg(env_slug))
  )
ORDER BY v.name;
