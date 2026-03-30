-- name: FireDueTimers :many
UPDATE timers SET fired = true
WHERE id IN (
    SELECT id FROM timers
    WHERE fired = false AND fires_at <= now()
    ORDER BY fires_at
    LIMIT 100
    FOR UPDATE SKIP LOCKED
)
RETURNING id, workflow_id, step_name, timer_type;

-- name: CancelTimer :exec
UPDATE timers SET fired = true
WHERE workflow_id = $1 AND step_name = $2 AND timer_type = $3 AND fired = false;

-- name: CancelAllWorkflowTimers :exec
UPDATE timers SET fired = true WHERE workflow_id = $1 AND fired = false;

-- name: CreateTimer :exec
INSERT INTO timers (workflow_id, step_name, timer_type, fires_at)
VALUES ($1, $2, $3, now() + make_interval(secs := $4))
ON CONFLICT (workflow_id, step_name, timer_type) DO NOTHING;

-- name: UpsertTimer :exec
INSERT INTO timers (workflow_id, step_name, timer_type, fires_at)
VALUES ($1, $2, $3, now() + make_interval(secs := $4))
ON CONFLICT (workflow_id, step_name, timer_type)
DO UPDATE SET fires_at = EXCLUDED.fires_at, fired = false;

-- name: CleanupFiredTimers :exec
DELETE FROM timers WHERE fired = true AND created_at < now() - interval '1 hour';
