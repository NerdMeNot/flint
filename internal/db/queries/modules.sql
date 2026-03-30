-- name: ListModules :many
SELECT id, name, description, oci_ref, schema_version, created_at
FROM pipeline_modules ORDER BY name;
