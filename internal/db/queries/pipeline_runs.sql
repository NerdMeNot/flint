-- name: ListRunsByProject :many
SELECT id, workflow_file, trigger_type, trigger_ref, commit_sha,
       commit_message, triggered_by, status, started_at, finished_at, duration_ms
FROM pipeline_runs
WHERE project_id = $1
ORDER BY started_at DESC
LIMIT $2;

-- name: ListRunsAll :many
SELECT pr.id, pr.project_id, p.display_name AS project_name, p.colour AS project_colour,
       pr.status, pr.trigger_type, pr.trigger_ref, pr.commit_sha,
       pr.triggered_by, pr.started_at, pr.duration_ms
FROM pipeline_runs pr
JOIN projects p ON pr.project_id = p.id
ORDER BY pr.started_at DESC
LIMIT $1;

-- name: GetRun :one
SELECT id, project_id, workflow_file, trigger_type, trigger_ref, commit_sha,
       commit_message, triggered_by, status, started_at, finished_at, duration_ms, workflow_id
FROM pipeline_runs
WHERE id = $1;

-- name: InsertPipelineRun :exec
INSERT INTO pipeline_runs (id, project_id, org_id, workflow_file,
    trigger_type, trigger_ref, commit_sha, commit_message, triggered_by, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'pending');

-- name: InsertManualRun :exec
INSERT INTO pipeline_runs (id, project_id, org_id, workflow_file,
    trigger_type, trigger_ref, triggered_by, status)
VALUES ($1, $2, $3, $4, 'manual', $5, 'api', 'pending');

-- name: InsertRetryRun :exec
INSERT INTO pipeline_runs (id, project_id, org_id, workflow_file,
    trigger_type, trigger_ref, commit_sha, triggered_by, status)
VALUES ($1, $2, $3, $4, 'retry', $5, $6, 'api', 'pending');

-- name: UpdateRunStatus :exec
UPDATE pipeline_runs SET status = $2 WHERE id = $1;

-- name: UpdateRunWorkflow :execrows
UPDATE pipeline_runs SET workflow_id = $2, branch = $3, repo = $4, status = 'running' WHERE id = $1;

-- name: FinishRun :exec
UPDATE pipeline_runs SET status = $2, finished_at = now(),
    duration_ms = EXTRACT(EPOCH FROM (now() - started_at)) * 1000
WHERE workflow_id = $1;

-- name: GetRunOrgID :one
SELECT org_id FROM pipeline_runs WHERE id = $1;

-- name: RunExists :one
SELECT EXISTS(SELECT 1 FROM pipeline_runs WHERE id = $1);

-- name: GetRunWorkflowID :one
SELECT workflow_id FROM pipeline_runs WHERE id = $1;

-- name: GetOriginalRunParams :one
SELECT project_id, org_id, workflow_file, trigger_ref, commit_sha
FROM pipeline_runs WHERE id = $1;
