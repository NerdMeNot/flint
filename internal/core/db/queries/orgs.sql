-- name: GetOrg :one
SELECT id, name, slug, concurrency_limit, require_project_workspace, sso_strict_groups FROM orgs LIMIT 1;

-- name: SetOrgRequireProjectWorkspace :exec
UPDATE orgs SET require_project_workspace = @require WHERE id = @id;

-- name: SetOrgStrictGroups :exec
UPDATE orgs SET sso_strict_groups = @strict WHERE id = @id;

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

-- name: LockOrgConcurrency :exec
-- Transaction-scoped advisory lock keyed by org. Held across the count + throttle
-- decision so two workers can't both observe headroom and both dispatch past the
-- org's concurrency limit. Auto-released at transaction end; per-org, so different
-- orgs never block each other.
SELECT pg_advisory_xact_lock(hashtext('flint-conc:' || sqlc.arg(org_id)::text));
