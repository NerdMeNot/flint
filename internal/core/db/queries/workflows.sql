-- name: InsertWorkflow :one
INSERT INTO workflows (run_id, parent_id, parent_step, status, input)
VALUES ($1, $2, $3, 'pending', $4)
RETURNING id;

-- name: GetExistingWorkflow :one
SELECT id FROM workflows WHERE run_id = $1 AND parent_id IS NULL;

-- name: LockWorkflow :one
SELECT status, dag_waves, step_outputs, input
FROM workflows WHERE id = $1 FOR UPDATE;

-- name: UpdateWorkflowPipeline :exec
UPDATE workflows SET pipeline_yaml = $2, pipeline_def = $3, dag_waves = $4,
    status = 'running', started_at = now()
WHERE id = $1;

-- name: FinishWorkflow :exec
UPDATE workflows SET status = $2, finished_at = now() WHERE id = $1;

-- name: CancelWorkflow :exec
UPDATE workflows SET status = 'cancelled', cancelled_at = now(), finished_at = now()
WHERE id = $1 AND status IN ('pending', 'running', 'paused');

-- name: PauseWorkflow :execrows
-- First-writer-wins: only a 'running' workflow can be paused. Returns rows
-- affected (0 = already paused/terminal, a no-op). While paused, ClaimQueuedSteps
-- skips its steps and advanceWorkflow queues nothing; in-flight steps still finish.
UPDATE workflows SET status = 'paused' WHERE id = $1 AND status = 'running';

-- name: ResumeWorkflow :execrows
-- Inverse of PauseWorkflow. The caller advances the workflow after resuming so
-- newly-eligible steps are queued.
UPDATE workflows SET status = 'running' WHERE id = $1 AND status = 'paused';

-- name: CancelChildWorkflows :exec
UPDATE workflows SET status = 'cancelled', cancelled_at = now(), finished_at = now()
WHERE parent_id = $1 AND status IN ('pending', 'running');

-- name: UpdateStepOutputs :exec
UPDATE workflows SET step_outputs = step_outputs || $2::jsonb WHERE id = $1;

-- name: GetWorkflowParent :one
SELECT parent_id, parent_step FROM workflows WHERE id = $1;

-- name: GetWorkflowStatus :one
SELECT run_id, status, started_at, finished_at FROM workflows WHERE id = $1;

-- name: GetWorkflowDAGWaves :one
SELECT dag_waves FROM workflows WHERE id = $1;

-- name: GetWorkflowInput :one
SELECT input FROM workflows WHERE id = $1;

-- name: GetWorkflowStepOutputs :one
-- The accumulated step_outputs map (stepName → StepResult) for a workflow. Used by
-- re-run-failed / retry-from-step to seed carried-over results into the new run.
SELECT step_outputs FROM workflows WHERE id = $1;

-- name: GetWorkflowInputs :many
-- Batch variant: fetch inputs for all workflows in a claimed step batch in one
-- round-trip (kills the per-step N+1 in claimAndDispatch).
SELECT id, input FROM workflows WHERE id = ANY(sqlc.arg(workflow_ids)::uuid[]);

-- name: SweepStaleWorkflows :exec
UPDATE workflows SET status = 'failed', finished_at = now()
WHERE status = 'running'
AND NOT EXISTS (
    SELECT 1 FROM steps
    WHERE workflow_id = workflows.id
    AND status NOT IN ('succeeded', 'failed', 'skipped', 'cancelled')
);

-- name: RecentlyFinishedRunIDs :many
SELECT DISTINCT pr.id AS run_id FROM pipeline_runs pr
JOIN workflows w ON w.run_id = pr.id
WHERE w.status IN ('succeeded', 'failed', 'cancelled')
AND w.finished_at >= now() - interval '10 minutes'
AND w.parent_id IS NULL;
