-- name: ListTagKeys :many
SELECT id, key, label, allowed_values, color, created_at
FROM tag_keys WHERE org_id = $1 ORDER BY key;

-- name: CreateTagKey :one
INSERT INTO tag_keys (org_id, key, label, allowed_values, color)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: UpdateTagKey :exec
UPDATE tag_keys
SET label = $2, allowed_values = $3, color = $4
WHERE id = $1;

-- name: DeleteTagKey :execrows
DELETE FROM tag_keys WHERE id = $1;
