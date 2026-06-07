-- name: CreateWorkflowSchedule :one
INSERT INTO workflow_schedules (org_id, name, cron, definition, next_run_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id;

-- name: ListWorkflowSchedules :many
SELECT id, name, cron, enabled, next_run_at, last_run_at, created_at
FROM workflow_schedules
WHERE org_id = $1
ORDER BY created_at DESC;

-- name: ListDueWorkflowSchedules :many
SELECT id, org_id, name, cron, definition
FROM workflow_schedules
WHERE enabled AND next_run_at <= now()
ORDER BY next_run_at
LIMIT 50;

-- name: AdvanceWorkflowScheduleIfDue :execrows
-- Atomically claim a due schedule by moving its next_run_at forward. Returns the
-- number of rows updated (1 = this caller won the claim, 0 = already advanced by
-- another worker), which makes firing multi-worker safe.
UPDATE workflow_schedules
SET next_run_at = $2, last_run_at = now(), updated_at = now()
WHERE id = $1 AND next_run_at <= now();
