-- name: ListWorkspaces :many
SELECT id, name, slug, description, created_at, is_default
FROM workspaces WHERE org_id = $1 ORDER BY is_default DESC, name
LIMIT $2 OFFSET $3;

-- name: GetWorkspaceBySlug :one
SELECT id, name, slug, description, created_at, is_default
FROM workspaces WHERE org_id = $1 AND slug = $2;

-- name: GetWorkspaceByID :one
SELECT id, name, slug, description, created_at, is_default
FROM workspaces WHERE id = $1;

-- name: CreateWorkspace :one
INSERT INTO workspaces (org_id, name, slug, description)
VALUES ($1, $2, $3, $4) RETURNING id;

-- name: EnsureDefaultWorkspace :exec
-- Idempotent: creates a "Default" workspace for the org if it has none. Called
-- at startup so every org always has a landing workspace for new projects.
INSERT INTO workspaces (org_id, name, slug, is_default)
SELECT sqlc.arg('org_id'), 'Default', 'default', true
WHERE NOT EXISTS (
    SELECT 1 FROM workspaces WHERE org_id = sqlc.arg('org_id') AND is_default
);

-- name: DeleteWorkspace :execrows
-- The default workspace can't be deleted (projects must always have a home).
DELETE FROM workspaces WHERE id = $1 AND is_default = false;

-- name: GetProjectWorkspaceSlug :one
SELECT w.slug FROM workspaces w
JOIN projects p ON p.workspace_id = w.id
WHERE p.id = $1;
