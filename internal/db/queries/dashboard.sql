-- name: GetDashboardSummary :many
SELECT p.id, p.display_name, p.repo_path, p.colour, p.tags, p.default_branch,
       lr.id AS latest_run_id, lr.status AS latest_run_status, lr.trigger_ref,
       lr.triggered_by, lr.started_at AS latest_run_started_at, lr.duration_ms
FROM projects p
LEFT JOIN LATERAL (
    SELECT id, status, trigger_ref, triggered_by, started_at, duration_ms
    FROM pipeline_runs
    WHERE project_id = p.id
    ORDER BY started_at DESC
    LIMIT 1
) lr ON true
WHERE p.is_archived = false
ORDER BY p.display_name, p.repo_path;

-- name: CountActiveProjects :one
SELECT COUNT(*) FROM projects WHERE is_archived = false;

-- name: CountRunningPipelines :one
SELECT COUNT(*) FROM pipeline_runs WHERE status = 'running';

-- name: CountPendingGates :one
SELECT COUNT(*) FROM steps WHERE exec_type = 'gate' AND status = 'waiting';

-- name: GetDashboardActivity :many
SELECT pr.id, pr.project_id, p.display_name AS project_name, p.colour AS project_colour,
       pr.status, pr.trigger_type, pr.trigger_ref, pr.commit_sha,
       pr.triggered_by, pr.started_at, pr.duration_ms
FROM pipeline_runs pr
JOIN projects p ON pr.project_id = p.id
ORDER BY pr.started_at DESC
LIMIT $1;
