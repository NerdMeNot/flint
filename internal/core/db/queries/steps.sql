-- name: InsertStep :exec
INSERT INTO steps (workflow_id, name, exec_type, status, wave, max_attempts,
    step_def, timeout_seconds, retry_backoff, retry_interval_seconds, on_failure)
VALUES ($1, $2, $3, 'pending', $4, $5, $6, $7, $8, $9, $10);

-- name: LockStep :one
SELECT id, status, max_attempts, retry_backoff, retry_interval_seconds
FROM steps
WHERE workflow_id = $1 AND name = $2 AND attempt = $3
FOR UPDATE;

-- name: UpdateStepResult :exec
UPDATE steps SET status = $2, result = $3, finished_at = now() WHERE id = $1;

-- name: SetStepStatus :exec
UPDATE steps SET status = $2 WHERE id = $1;

-- name: SetStepQueued :exec
UPDATE steps SET status = 'queued', queued_at = now() WHERE id = $1;

-- name: SetStepSkipped :exec
UPDATE steps SET status = 'skipped', finished_at = now() WHERE id = $1;

-- name: CancelPendingSteps :exec
UPDATE steps SET status = 'cancelled', finished_at = now()
WHERE workflow_id = $1 AND status NOT IN ('succeeded', 'failed', 'skipped', 'cancelled');

-- name: LatestStepsByWorkflow :many
SELECT DISTINCT ON (name) id, name, status, on_failure,
    step_def->>'if' AS if_condition,
    step_def->>'when' AS when_condition
FROM steps WHERE workflow_id = $1
ORDER BY name, attempt DESC;

-- name: ListStepsByWorkflow :many
SELECT DISTINCT ON (name) name, status, wave, attempt, max_attempts, result,
    exec_type, started_at, finished_at,
    step_def->'dependsOn' AS depends_on
FROM steps WHERE workflow_id = $1
ORDER BY name, attempt DESC;

-- name: ClaimQueuedSteps :many
UPDATE steps SET
    status = CASE WHEN exec_type = 'gate' THEN 'waiting' ELSE 'running' END,
    started_at = now(),
    deadline_at = now() + make_interval(secs := timeout_seconds)
WHERE id IN (
    SELECT id FROM steps
    WHERE status = 'queued'
    ORDER BY wave, queued_at
    LIMIT $1
    FOR UPDATE SKIP LOCKED
)
RETURNING id, workflow_id, name, exec_type, attempt, step_def, status;

-- name: SetStepTaskToken :exec
UPDATE steps SET task_token = $2 WHERE id = $1;

-- name: SetStepK8sJobName :exec
UPDATE steps SET k8s_job_name = $2 WHERE id = $1;

-- name: CreateRetryStep :exec
INSERT INTO steps (workflow_id, name, exec_type, status, wave, attempt,
    max_attempts, step_def, timeout_seconds, retry_backoff, retry_interval_seconds, on_failure)
SELECT s.workflow_id, s.name, s.exec_type, 'pending', s.wave, sqlc.arg(new_attempt),
    s.max_attempts, s.step_def, s.timeout_seconds, s.retry_backoff, s.retry_interval_seconds, s.on_failure
FROM steps s WHERE s.id = sqlc.arg(source_step_id);

-- name: FailStepByTimeout :exec
UPDATE steps SET status = 'failed', result = sqlc.arg(result)::jsonb, finished_at = now()
WHERE workflow_id = sqlc.arg(workflow_id) AND name = sqlc.arg(step_name) AND status = 'running';

-- name: FailGateByTimeout :exec
UPDATE steps SET status = 'failed', result = sqlc.arg(result)::jsonb, finished_at = now()
WHERE workflow_id = sqlc.arg(workflow_id) AND name = sqlc.arg(step_name) AND status = 'waiting';

-- name: RequeueRetryStep :exec
WITH latest AS (
    SELECT s.id FROM steps s
    WHERE s.workflow_id = $1 AND s.name = $2 AND s.status = 'pending'
    ORDER BY s.attempt DESC LIMIT 1
)
UPDATE steps SET status = 'queued', queued_at = now()
FROM latest WHERE steps.id = latest.id;

-- name: CompleteParentInvokeStep :exec
UPDATE steps SET status = sqlc.arg(new_status), result = sqlc.arg(result), finished_at = now()
WHERE workflow_id = sqlc.arg(workflow_id) AND name = sqlc.arg(step_name) AND status = 'running';

-- name: SweepStaleRunningSteps :execrows
UPDATE steps SET status = 'failed',
    result = ('{"stepName":"' || name || '","success":false,"error":"step timed out (sweep)"}')::jsonb,
    finished_at = now()
WHERE status = 'running' AND deadline_at < now();

-- name: RecentlyFailedWorkflowIDs :many
SELECT DISTINCT workflow_id FROM steps
WHERE status = 'failed' AND finished_at >= now() - interval '10 seconds';

-- name: GetStepStatus :one
SELECT s.status FROM steps s
JOIN workflows w ON s.workflow_id = w.id
JOIN pipeline_runs pr ON w.run_id = pr.id
WHERE pr.id = $1 AND s.name = $2
ORDER BY s.attempt DESC LIMIT 1;

-- name: ListPendingGates :many
SELECT s.name AS step_name,
       s.step_def->'gate'->>'message' AS message,
       w.id AS workflow_id,
       pr.id AS run_id, pr.trigger_ref AS branch, pr.triggered_by,
       p.id AS project_id, p.display_name AS project_name, p.colour AS project_colour,
       s.created_at
FROM steps s
JOIN workflows w ON s.workflow_id = w.id
JOIN pipeline_runs pr ON w.run_id = pr.id
JOIN projects p ON pr.project_id = p.id
WHERE s.exec_type = 'gate' AND s.status = 'waiting'
ORDER BY s.created_at ASC
LIMIT 50;

-- name: GetStepByWorkflowAndName :one
SELECT id, workflow_id, name, exec_type, step_def FROM steps
WHERE workflow_id = $1 AND name = $2
ORDER BY attempt DESC LIMIT 1;

-- name: ListWaitingGatesWithSignals :many
SELECT s.id AS step_id, s.workflow_id, s.name AS step_name,
       sig.id AS signal_id, sig.payload
FROM steps s
JOIN signals sig ON sig.workflow_id = s.workflow_id
    AND sig.signal_name = 'gate-' || s.name
    AND sig.consumed = false
WHERE s.status = 'waiting' AND s.exec_type = 'gate'
LIMIT 50;

-- name: ListWaitingGatesWithRejectSignals :many
SELECT s.id AS step_id, s.workflow_id, s.name AS step_name,
       sig.id AS signal_id, sig.payload
FROM steps s
JOIN signals sig ON sig.workflow_id = s.workflow_id
    AND sig.signal_name = 'gate-reject-' || s.name
    AND sig.consumed = false
WHERE s.status = 'waiting' AND s.exec_type = 'gate'
LIMIT 50;
