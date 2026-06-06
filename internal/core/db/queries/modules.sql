-- name: ListModules :many
SELECT id, name, description, oci_ref, schema_version, created_at
FROM pipeline_modules ORDER BY name;

-- name: GetModuleByName :one
SELECT id, name, oci_ref, inputs_schema, outputs_schema
FROM pipeline_modules WHERE name = $1;
