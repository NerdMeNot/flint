-- name: GetProtectedEnvironment :one
SELECT id, name, min_role, approvers, deploy_branches, deploy_window, created_at
FROM protected_environments WHERE org_id = $1 AND name = $2;

-- name: ListProtectedEnvironments :many
SELECT id, name, min_role, approvers, deploy_branches, deploy_window, created_at
FROM protected_environments WHERE org_id = $1 ORDER BY name;

-- name: CreateProtectedEnvironment :one
INSERT INTO protected_environments (org_id, name, min_role, approvers, deploy_branches, deploy_window)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING id;

-- name: UpdateProtectedEnvironment :execrows
UPDATE protected_environments SET
    min_role = $2, approvers = $3, deploy_branches = $4, deploy_window = $5
WHERE id = $1;

-- name: DeleteProtectedEnvironment :execrows
DELETE FROM protected_environments WHERE id = $1;
