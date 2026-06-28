-- name: LockNextDueTimer :one
-- Claims one due timer, locking the row (FOR UPDATE SKIP LOCKED) for the duration
-- of the caller's transaction. The caller handles the timer's effect and marks it
-- fired in the SAME transaction, so `fired = true` only commits if the effect
-- commits — a crash mid-handle rolls back and the timer is retried next tick
-- (exactly-once handling, not the previous at-most-once).
SELECT id, workflow_id, step_name, timer_type
FROM timers
WHERE fired = false AND fires_at <= now()
ORDER BY fires_at
LIMIT 1
FOR UPDATE SKIP LOCKED;

-- name: MarkTimerFired :exec
UPDATE timers SET fired = true WHERE id = $1;

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
