-- name: ListRunsByProject :many
SELECT id, workflow_file, trigger_type, trigger_ref, commit_sha,
       commit_message, triggered_by, status, started_at, finished_at, duration_ms,
       error_message
FROM pipeline_runs
WHERE project_id = $1
ORDER BY started_at DESC
LIMIT $2;

-- name: ListRunsAll :many
SELECT pr.id, pr.project_id, p.display_name AS project_name, p.colour AS project_colour,
       pr.status, pr.trigger_type, pr.trigger_ref, pr.commit_sha,
       pr.triggered_by, pr.started_at, pr.duration_ms, pr.error_message
FROM pipeline_runs pr
JOIN projects p ON pr.project_id = p.id
ORDER BY pr.started_at DESC
LIMIT $1;

-- name: GetRun :one
SELECT id, project_id, workflow_file, trigger_type, trigger_ref, commit_sha,
       commit_message, triggered_by, status, started_at, finished_at, duration_ms,
       workflow_id, environment, error_message
FROM pipeline_runs
WHERE id = $1;

-- name: InsertPipelineRun :exec
INSERT INTO pipeline_runs (id, project_id, org_id, workflow_file,
    trigger_type, trigger_ref, commit_sha, commit_message, triggered_by,
    environment, status)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'pending');

-- name: InsertManualRun :exec
INSERT INTO pipeline_runs (id, project_id, org_id, workflow_file,
    trigger_type, trigger_ref, triggered_by, environment, status)
VALUES ($1, $2, $3, $4, 'manual', $5, 'api', $6, 'pending');

-- name: InsertWorkflowRun :exec
-- A non-CI workflow run: no project, no pipeline file. kind marks it 'workflow'.
INSERT INTO pipeline_runs (id, org_id, kind, trigger_type, triggered_by, status)
VALUES ($1, $2, 'workflow', $3, $4, 'pending');

-- name: InsertRetryRun :exec
INSERT INTO pipeline_runs (id, project_id, org_id, workflow_file,
    trigger_type, trigger_ref, commit_sha, triggered_by, environment, status)
VALUES ($1, $2, $3, $4, 'retry', $5, $6, 'api', $7, 'pending');

-- name: UpdateRunStatus :exec
UPDATE pipeline_runs SET status = $2 WHERE id = $1;

-- name: FailRunWithError :exec
UPDATE pipeline_runs SET status = 'failed', error_message = $2,
    finished_at = now(), duration_ms = EXTRACT(EPOCH FROM (now() - started_at)) * 1000
WHERE id = $1;

-- name: UpdateRunWorkflow :execrows
UPDATE pipeline_runs SET workflow_id = $2, branch = $3, repo = $4, status = 'running' WHERE id = $1;

-- name: FinishRun :exec
UPDATE pipeline_runs SET status = $2, finished_at = now(),
    duration_ms = EXTRACT(EPOCH FROM (now() - started_at)) * 1000
WHERE workflow_id = $1;

-- name: GetRunOrgID :one
SELECT org_id FROM pipeline_runs WHERE id = $1;

-- name: RunsNeedingCleanup :many
-- Runs that reached a terminal state but whose executor resources (workspace
-- pod, leftover Jobs) haven't been torn down yet. The loop claims these and
-- calls each executor's CleanupRun, then marks them cleaned — exactly-once.
SELECT id FROM pipeline_runs
WHERE cleaned_at IS NULL AND status IN ('succeeded', 'failed', 'cancelled')
LIMIT $1;

-- name: MarkRunCleaned :exec
UPDATE pipeline_runs SET cleaned_at = now() WHERE id = $1;

-- name: ListWorkflowRuns :many
-- Lists an org's workflow runs (kind = 'workflow'), newest first, with keyset
-- pagination. The cursor is (started_at, id); an empty cursor returns the first
-- page. Project-joining run queries can't serve these — workflow runs have no
-- project.
SELECT id, status, trigger_type, triggered_by, started_at, finished_at,
       duration_ms, error_message
FROM pipeline_runs
WHERE org_id = sqlc.arg('org_id') AND kind = 'workflow'
  AND ( sqlc.arg('cursor_ts')::text = ''
        OR started_at < sqlc.arg('cursor_ts')::timestamptz
        OR (started_at = sqlc.arg('cursor_ts')::timestamptz AND id < sqlc.arg('cursor_id')) )
ORDER BY started_at DESC, id DESC
LIMIT sqlc.arg('lim');

-- name: GetRunScope :one
-- Resolves the RBAC scope (workspace slug + environment) for a run, used by the
-- authorization middleware. Workflow runs have no project, so both fall back to
-- the empty string (the caller treats "" as the global "*" scope).
SELECT COALESCE(w.slug, '')::text AS workspace_slug,
       COALESCE(pr.environment, '')::text AS environment
FROM pipeline_runs pr
LEFT JOIN projects p ON p.id = pr.project_id
LEFT JOIN workspaces w ON w.id = p.workspace_id
WHERE pr.id = $1;

-- name: RunExists :one
SELECT EXISTS(SELECT 1 FROM pipeline_runs WHERE id = $1);

-- name: GetRunWorkflowID :one
SELECT workflow_id FROM pipeline_runs WHERE id = $1;

-- name: GetOriginalRunParams :one
SELECT project_id, org_id, workflow_file, trigger_ref, commit_sha, environment
FROM pipeline_runs WHERE id = $1;
