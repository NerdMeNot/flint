-- name: ListWorkspaces :many
SELECT w.id, w.name, w.slug, w.description, w.created_at,
       w.owner_team_id, t.name AS owner_team_name, t.slug AS owner_team_slug
FROM workspaces w
LEFT JOIN teams t ON t.id = w.owner_team_id
WHERE w.org_id = $1 ORDER BY w.name
LIMIT $2 OFFSET $3;

-- name: GetWorkspaceBySlug :one
SELECT w.id, w.name, w.slug, w.description, w.created_at,
       w.owner_team_id, t.name AS owner_team_name, t.slug AS owner_team_slug
FROM workspaces w
LEFT JOIN teams t ON t.id = w.owner_team_id
WHERE w.org_id = $1 AND w.slug = $2;

-- name: GetWorkspaceByID :one
SELECT w.id, w.name, w.slug, w.description, w.created_at,
       w.owner_team_id, t.name AS owner_team_name, t.slug AS owner_team_slug
FROM workspaces w
LEFT JOIN teams t ON t.id = w.owner_team_id
WHERE w.id = $1;

-- name: CreateWorkspace :one
INSERT INTO workspaces (org_id, name, slug, description)
VALUES ($1, $2, $3, $4) RETURNING id;

-- name: SetWorkspaceOwnerTeam :exec
UPDATE workspaces SET owner_team_id = sqlc.narg('owner_team_id') WHERE id = sqlc.arg('id');

-- name: DeleteWorkspace :execrows
DELETE FROM workspaces WHERE id = $1;

-- name: GetProjectWorkspaceSlug :one
SELECT w.slug FROM workspaces w
JOIN projects p ON p.workspace_id = w.id
WHERE p.id = $1;
