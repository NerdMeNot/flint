-- name: ListSavedViews :many
SELECT id, name, route, selector, created_at
FROM saved_views
WHERE org_id = $1 AND owner_user_id = $2
ORDER BY created_at DESC;

-- name: CreateSavedView :one
INSERT INTO saved_views (org_id, owner_user_id, name, route, selector)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: DeleteSavedView :execrows
DELETE FROM saved_views
WHERE id = $1 AND org_id = $2 AND owner_user_id = $3;
