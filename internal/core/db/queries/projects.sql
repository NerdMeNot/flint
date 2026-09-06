-- name: ListProjects :many
SELECT id, repo_path, display_name, description, colour, tags, default_branch, is_archived, created_at
FROM projects
WHERE is_archived = false
ORDER BY display_name, repo_path;

-- name: ListProjectsWithLastRun :many
-- API project list: joins owning workspace + latest run, with optional
-- server-side workspace and tag filters (empty slice = no filter for that axis).
SELECT p.id, COALESCE(p.display_name, p.repo_path)::text AS name, p.repo_path,
       COALESCE(w.slug, '')::text AS workspace, p.colour, p.tags, p.created_at,
       p.workspace_inferred AS inferred,
       -- COALESCE the NOT-NULL-typed columns to sentinels: a project with no runs
       -- yields NULLs from the LEFT JOIN, and the handler treats ''/zero as "no
       -- run" (guarded on last_run_id). trigger_ref/triggered_by/duration_ms are
       -- already nullable in the schema.
       COALESCE(lr.id::text, '')::text AS last_run_id,
       COALESCE(lr.status, '')::text AS last_run_status,
       lr.trigger_ref AS last_run_branch, lr.triggered_by AS last_run_triggered_by,
       COALESCE(lr.started_at, 'epoch'::timestamptz) AS last_run_started_at,
       lr.duration_ms AS last_run_duration_ms
FROM projects p
LEFT JOIN workspaces w ON w.id = p.workspace_id
-- Regular LEFT JOIN (not LATERAL) so sqlc infers the last-run columns as
-- nullable — projects with no runs yet must not break the scan. DISTINCT ON
-- keeps it a single indexed pass for the latest run per project.
LEFT JOIN (
    SELECT DISTINCT ON (project_id)
           project_id, id, status, trigger_ref, triggered_by, started_at, duration_ms
    FROM pipeline_runs
    ORDER BY project_id, started_at DESC
) lr ON lr.project_id = p.id
WHERE p.is_archived = false
  AND (cardinality(@workspaces::text[]) = 0 OR w.slug = ANY(@workspaces::text[]))
  AND (cardinality(@tags::text[]) = 0 OR p.tags && @tags::text[])
  -- "needs grouping": workspace was inferred (not declared) or no tags.
  AND (NOT @needs_grouping::bool OR p.workspace_inferred OR cardinality(p.tags) = 0)
ORDER BY COALESCE(p.display_name, p.repo_path);

-- name: GetProject :one
SELECT id, repo_path, display_name, description, colour, tags, default_branch, is_archived, created_at
FROM projects
WHERE id = $1;

-- name: GetProjectByRepoPath :one
SELECT p.id, p.org_id, p.pipeline_source->>'path' AS pipeline_path
FROM projects p
JOIN forge_connections fc ON p.forge_id = fc.id
WHERE p.repo_path = $1 AND p.is_archived = false
LIMIT 1;

-- name: ArchiveProject :exec
UPDATE projects SET is_archived = true, updated_at = now() WHERE id = $1;

-- name: RestoreProject :exec
UPDATE projects SET is_archived = false, updated_at = now() WHERE id = $1;

-- name: ListArchivedProjects :many
-- Archived projects for the admin Projects page (so they can be restored).
-- Takes the same RBAC workspace restriction as the live list: this is reached
-- through GET /projects?archived=true, so a scope-limited caller authorized for
-- that route must not see rows from workspaces they cannot read. Empty slice =
-- unrestricted.
SELECT p.id, COALESCE(p.display_name, p.repo_path)::text AS name, p.repo_path,
       COALESCE(w.slug, '')::text AS workspace, p.colour, p.created_at
FROM projects p
LEFT JOIN workspaces w ON w.id = p.workspace_id
WHERE p.is_archived = true
  AND (cardinality(@workspaces::text[]) = 0 OR w.slug = ANY(@workspaces::text[]))
ORDER BY COALESCE(p.display_name, p.repo_path);

-- name: GetProjectConfig :one
-- Full editable config for a project, for the API PATCH-merge and archive cleanup.
-- forge_ref is the forge connection's display name (what UpsertProject keys on).
SELECT p.id, p.repo_path, fc.display_name AS forge_ref,
       p.display_name, p.description, p.colour, p.icon, p.default_branch,
       p.pipeline_source, p.forge_webhook_id,
       COALESCE(w.slug, '')::text AS workspace
FROM projects p
JOIN forge_connections fc ON p.forge_id = fc.id
LEFT JOIN workspaces w ON w.id = p.workspace_id
WHERE p.id = $1;

-- name: SetProjectWebhookID :exec
-- Record the inbound forge webhook id after provisioning (or clear it on removal).
UPDATE projects SET forge_webhook_id = @forge_webhook_id, updated_at = now() WHERE id = @id;

-- name: GetProjectOrgID :one
SELECT org_id FROM projects WHERE id = $1;

-- name: UpdateProjectTags :exec
-- UI-managed project labels (the registry curates the vocabulary; this stores
-- the chosen key:value and free tags on the project).
UPDATE projects SET tags = @tags, updated_at = now() WHERE id = @id;

-- name: UpsertProject :one
-- Workspace placement (the CRD is authoritative): if spec.workspace is declared
-- use that workspace (created on the fly if it doesn't exist); otherwise the
-- project lands in the org's default "Unsorted" workspace. workspace_inferred is
-- true when no workspace was declared (i.e. it defaulted to Unsorted).
WITH fc AS (
    SELECT f.id, f.org_id FROM forge_connections f WHERE f.display_name = @forge_ref LIMIT 1
),
target AS (
    SELECT
        fc.id AS forge_id,
        fc.org_id,
        (NULLIF(@workspace::text, '') IS NULL) AS inferred,
        NULLIF(@workspace::text, '') AS ws_slug
    FROM fc
),
ws AS (
    INSERT INTO workspaces (org_id, name, slug)
    SELECT org_id, ws_slug, ws_slug FROM target WHERE ws_slug IS NOT NULL
    ON CONFLICT (org_id, slug) DO UPDATE SET slug = EXCLUDED.slug
    RETURNING id
)
-- tags are intentionally NOT written here — they're UI-managed (see
-- UpdateProjectTags), so a reconcile never clobbers them.
INSERT INTO projects (
    org_id, forge_id, repo_path, repo_url, display_name, description,
    colour, icon, default_branch, pipeline_source, is_archived, updated_at,
    workspace_id, workspace_inferred
)
SELECT
    t.org_id, t.forge_id, @repo_path, @repo_url, @display_name, @description,
    @colour, @icon, @default_branch, @pipeline_source::jsonb, false, now(),
    COALESCE(
        (SELECT id FROM ws),
        (SELECT w.id FROM workspaces w WHERE w.org_id = t.org_id AND w.is_default LIMIT 1)
    ),
    t.inferred
FROM target t
ON CONFLICT (forge_id, repo_path)
DO UPDATE SET
    display_name = EXCLUDED.display_name,
    description = EXCLUDED.description,
    colour = EXCLUDED.colour,
    icon = EXCLUDED.icon,
    default_branch = EXCLUDED.default_branch,
    pipeline_source = EXCLUDED.pipeline_source,
    is_archived = false,
    updated_at = now(),
    workspace_id = EXCLUDED.workspace_id,
    workspace_inferred = EXCLUDED.workspace_inferred
RETURNING id;

-- name: GetProjectRepoInfo :one
SELECT repo_path, org_id, COALESCE(pipeline_source->>'path', '.flint/')::text AS pipeline_path
FROM projects WHERE id = $1;

-- name: SearchProjects :many
-- Project search by name / repo for the global ⌘K search.
SELECT p.id, COALESCE(p.display_name, p.repo_path)::text AS name, p.repo_path, p.colour
FROM projects p
LEFT JOIN workspaces w ON w.id = p.workspace_id
WHERE p.org_id = sqlc.arg('org_id') AND p.is_archived = false
  AND (p.display_name ILIKE sqlc.arg('pattern') OR p.repo_path ILIKE sqlc.arg('pattern'))
  -- RBAC workspace restriction; empty slice = unrestricted. Search is a
  -- collection route like any other: it must not surface rows the caller
  -- cannot open.
  AND (cardinality(@workspaces::text[]) = 0 OR w.slug = ANY(@workspaces::text[]))
ORDER BY p.display_name
LIMIT 10;

-- name: GetProjectBasic :one
-- Single project with workspace slug. The most recent run is fetched separately
-- (ListRunsByProject with limit 1) to keep nullability clean.
SELECT p.id, COALESCE(p.display_name, p.repo_path)::text AS name, p.repo_path,
       COALESCE(w.slug, '')::text AS workspace, p.colour, p.tags, p.created_at
FROM projects p
LEFT JOIN workspaces w ON w.id = p.workspace_id
WHERE p.id = $1;

-- name: ProjectHealthByOrg :many
-- Per-project run health for an org: recent statuses (newest first, capped at 10)
-- plus totals — powers the dashboard health bars / "needs attention".
SELECT project_id::text AS project_id,
       (array_agg(status ORDER BY started_at DESC))[1:10]::text[] AS recent_statuses,
       COUNT(*) AS total_runs,
       COUNT(*) FILTER (WHERE status = 'succeeded') AS succeeded_runs
FROM pipeline_runs
WHERE org_id = $1 AND project_id IS NOT NULL
GROUP BY project_id;

-- name: ProjectHealthByID :one
SELECT (array_agg(status ORDER BY started_at DESC))[1:10]::text[] AS recent_statuses,
       COUNT(*) AS total_runs,
       COUNT(*) FILTER (WHERE status = 'succeeded') AS succeeded_runs
FROM pipeline_runs WHERE project_id = $1;
