-- ────────────────────────────────────────────────────────────
-- API key workspace scope
-- ────────────────────────────────────────────────────────────

-- name: ListAPIKeyWorkspaceSlugs :many
SELECT w.slug FROM api_key_workspace_scope akws
JOIN workspaces w ON w.id = akws.workspace_id
WHERE akws.api_key_id = $1;

-- name: InsertAPIKeyWorkspaceScope :exec
INSERT INTO api_key_workspace_scope (api_key_id, workspace_id)
SELECT $1, id FROM workspaces WHERE slug = $2
ON CONFLICT DO NOTHING;

-- name: DeleteAPIKeyWorkspaceScopes :exec
DELETE FROM api_key_workspace_scope WHERE api_key_id = $1;

-- ────────────────────────────────────────────────────────────
-- API key environment scope
-- ────────────────────────────────────────────────────────────

-- name: ListAPIKeyEnvironmentSlugs :many
SELECT e.slug FROM api_key_environment_scope akes
JOIN environments e ON e.id = akes.environment_id
WHERE akes.api_key_id = $1;

-- name: InsertAPIKeyEnvironmentScope :exec
INSERT INTO api_key_environment_scope (api_key_id, environment_id)
SELECT $1, id FROM environments WHERE slug = $2
ON CONFLICT DO NOTHING;

-- name: DeleteAPIKeyEnvironmentScopes :exec
DELETE FROM api_key_environment_scope WHERE api_key_id = $1;
