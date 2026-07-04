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

-- name: ListRunsFiltered :many
-- Global CI run list with optional project/status filters, joined to the project
-- for display, plus a compact per-step summary (name+status) the UI renders as
-- stage pips. Powers GET /api/v1/runs.
SELECT pr.id, pr.status, pr.started_at, pr.finished_at, pr.duration_ms,
       pr.workflow_file, pr.trigger_ref AS branch, pr.trigger_type,
       pr.commit_sha, pr.commit_message, pr.triggered_by, pr.environment,
       pr.error_message,
       p.display_name AS project_name, p.id AS project_id,
       p.colour AS project_colour, p.repo_path,
       COALESCE((
         SELECT json_agg(json_build_object('name', s.name, 'status', s.status) ORDER BY s.wave, s.name)
         FROM steps s WHERE s.workflow_id = pr.workflow_id
       ), '[]')::jsonb AS steps
FROM pipeline_runs pr
JOIN projects p ON p.id = pr.project_id
WHERE (sqlc.arg('project_id')::text = '' OR pr.project_id::text = sqlc.arg('project_id'))
  AND (sqlc.arg('status')::text = '' OR pr.status = sqlc.arg('status'))
  AND (
    sqlc.arg('cursor_ts')::text = ''
    -- NULLIF(...::text,'') keeps the param TEXT so pgx binds an empty first-page
    -- cursor without trying (and failing) to encode '' as a uuid.
    OR (pr.started_at, pr.id) < (sqlc.arg('cursor_ts')::timestamptz, NULLIF(sqlc.arg('cursor_id')::text, '')::uuid)
  )
ORDER BY pr.started_at DESC, pr.id DESC
LIMIT sqlc.arg('lim');

-- name: GetRunDetail :one
-- Single CI run with project display fields + the per-step summary, for
-- GET /api/v1/runs/:id.
SELECT pr.id, pr.status, pr.started_at, pr.finished_at, pr.duration_ms,
       pr.workflow_file, pr.trigger_ref AS branch, pr.trigger_type,
       pr.commit_sha, pr.commit_message, pr.triggered_by, pr.environment,
       pr.error_message,
       p.display_name AS project_name, p.id AS project_id,
       p.colour AS project_colour, p.repo_path,
       COALESCE((
         SELECT json_agg(json_build_object('name', s.name, 'status', s.status) ORDER BY s.wave, s.name)
         FROM steps s WHERE s.workflow_id = pr.workflow_id
       ), '[]')::jsonb AS steps
FROM pipeline_runs pr
JOIN projects p ON p.id = pr.project_id
WHERE pr.id = $1;

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

-- name: SetRunStatusByWorkflow :exec
-- Sets the run row's status from its workflow (used for pause/resume so the run
-- list and detail reflect the paused state). Does not touch finished_at.
UPDATE pipeline_runs SET status = sqlc.arg(status) WHERE workflow_id = sqlc.arg(workflow_id);

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
        -- NULLIF(...::text,'') keeps the param TEXT so an empty first-page cursor
        -- binds without pgx trying to encode '' as a uuid.
        OR (started_at = sqlc.arg('cursor_ts')::timestamptz AND id < NULLIF(sqlc.arg('cursor_id')::text, '')::uuid) )
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

-- name: SearchRuns :many
-- Run search by branch / commit SHA for the global ⌘K search.
SELECT pr.id, pr.status, pr.trigger_ref AS branch, pr.commit_sha,
       COALESCE(p.display_name, p.repo_path)::text AS project_name,
       p.colour AS project_colour
FROM pipeline_runs pr
JOIN projects p ON p.id = pr.project_id
WHERE pr.org_id = sqlc.arg('org_id')
  AND (pr.trigger_ref ILIKE sqlc.arg('pattern') OR pr.commit_sha ILIKE sqlc.arg('pattern'))
ORDER BY pr.started_at DESC
LIMIT 10;

-- name: SetRunConcurrencyGroup :exec
-- Stamps the resolved concurrency group (expressions already interpolated) on
-- a run so cancel-in-progress can find superseded runs in the same group.
UPDATE pipeline_runs SET concurrency_group = $2 WHERE id = $1;

-- name: ActiveRunsInConcurrencyGroup :many
-- Running runs in the same project+group, excluding the superseding run —
-- the cancel-in-progress candidates. Uses idx_runs_concurrency_group.
SELECT id, workflow_id FROM pipeline_runs
WHERE project_id = $1 AND concurrency_group = $2
  AND status = 'running' AND id != $3;

-- name: DeleteOldRuns :execrows
-- Runs retention: delete terminal runs older than the retention window, in
-- bounded batches so a long-lived install's first sweep doesn't stall. The
-- workflows/steps/signals/timers rows cascade via FKs; engine_events has no FK
-- (by design) and its own 30-day cleanup.
DELETE FROM pipeline_runs WHERE id IN (
    SELECT id FROM pipeline_runs
    WHERE status IN ('succeeded', 'failed', 'cancelled')
      AND finished_at < now() - make_interval(days => sqlc.arg(retention_days)::int)
    LIMIT 500
);

-- name: GetRunStatusInfo :one
-- The fields needed to report a run's outcome to the forge (commit status /
-- check run) and to render status badges.
SELECT pr.repo, pr.commit_sha, pr.workflow_file, pr.status, pr.project_id
FROM pipeline_runs pr WHERE pr.id = $1;

-- name: LatestRunStatusForProject :one
-- The newest run's status for a project (optionally filtered by branch) — the
-- status badge source.
SELECT pr.status FROM pipeline_runs pr
WHERE pr.project_id = $1
  AND (sqlc.arg(branch)::text = '' OR pr.branch = sqlc.arg(branch)::text)
ORDER BY pr.created_at DESC LIMIT 1;
