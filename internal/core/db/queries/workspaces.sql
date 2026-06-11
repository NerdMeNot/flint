-- name: ListWorkspaces :many
SELECT id, name, slug, description, created_at
FROM workspaces WHERE org_id = $1 ORDER BY name
LIMIT $2 OFFSET $3;

-- name: GetWorkspaceBySlug :one
SELECT id, name, slug, description, created_at
FROM workspaces WHERE org_id = $1 AND slug = $2;

-- name: GetWorkspaceByID :one
SELECT id, name, slug, description, created_at
FROM workspaces WHERE id = $1;

-- name: CreateWorkspace :one
INSERT INTO workspaces (org_id, name, slug, description)
VALUES ($1, $2, $3, $4) RETURNING id;

-- name: DeleteWorkspace :execrows
DELETE FROM workspaces WHERE id = $1;

-- name: GetProjectWorkspaceSlug :one
SELECT w.slug FROM workspaces w
JOIN projects p ON p.workspace_id = w.id
WHERE p.id = $1;
