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
-- Idempotent: creates the fallback "Unsorted" workspace for the org if it has
-- none. Called at startup. Projects only land here when no workspace was
-- declared and none could be inferred — it's a triage bucket, not a home.
INSERT INTO workspaces (org_id, name, slug, is_default)
SELECT sqlc.arg('org_id'), 'Unsorted', 'unsorted', true
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
