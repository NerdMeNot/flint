-- name: GetOrg :one
SELECT id, name, slug, concurrency_limit, require_project_workspace FROM orgs LIMIT 1;

-- name: SetOrgRequireProjectWorkspace :exec
UPDATE orgs SET require_project_workspace = @require WHERE id = @id;

-- name: GetOrCreateDefaultOrg :one
INSERT INTO orgs (name, slug)
VALUES ('default', 'default')
ON CONFLICT (slug) DO UPDATE SET name = orgs.name
RETURNING id;

-- name: GetOrgConcurrencyLimit :one
SELECT concurrency_limit FROM orgs WHERE id = $1;

-- name: CountRunningStepsByOrg :one
SELECT COUNT(*) AS running_count
FROM steps s
JOIN workflows w ON s.workflow_id = w.id
JOIN pipeline_runs pr ON w.run_id = pr.id
WHERE pr.org_id = $1 AND s.status = 'running';
