-- name: ListProjectWebhooks :many
SELECT id, project_id, url, secret, events, is_active, created_at
FROM webhooks WHERE project_id = $1
ORDER BY created_at ASC;

-- name: CreateWebhook :one
INSERT INTO webhooks (project_id, url, secret, events)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: DeleteWebhook :exec
DELETE FROM webhooks WHERE id = $1 AND project_id = $2;

-- name: GetActiveWebhooksForEvent :many
SELECT w.id, w.url, w.secret
FROM webhooks w
JOIN projects p ON w.project_id = p.id
JOIN pipeline_runs pr ON pr.project_id = p.id
WHERE pr.id = $1
  AND w.is_active = true
  AND w.events @> to_jsonb($2::text);
