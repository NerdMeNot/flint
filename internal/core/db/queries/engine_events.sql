-- name: InsertEngineEvent :exec
-- Append one transition to the durable history log. Written in the same tx as the
-- state change it records (see internal/core/engine/transition.go). Append-only:
-- rows are never updated.
INSERT INTO engine_events (
    workflow_id, step_name, attempt, event_type,
    from_status, to_status, actor, reason, metadata
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: ListEngineEventsByWorkflow :many
-- Full transition timeline for a workflow (run), oldest first. Powers the run
-- timeline / step history UI.
SELECT id, workflow_id, step_name, attempt, event_type,
    from_status, to_status, actor, reason, metadata, created_at
FROM engine_events
WHERE workflow_id = $1
ORDER BY created_at ASC, id ASC;

-- name: ListEngineEventsByStep :many
-- Transition history for a single step across all attempts.
SELECT id, workflow_id, step_name, attempt, event_type,
    from_status, to_status, actor, reason, metadata, created_at
FROM engine_events
WHERE workflow_id = $1 AND step_name = $2
ORDER BY created_at ASC, id ASC;

-- name: CleanupOldEngineEvents :exec
-- Prune transition history older than 30 days so the table stays bounded. Called
-- from the sweep alongside the other retention cleanups.
DELETE FROM engine_events WHERE created_at < now() - interval '30 days';
