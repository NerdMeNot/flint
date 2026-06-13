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
       lr.id AS last_run_id, lr.status AS last_run_status,
       lr.trigger_ref AS last_run_branch, lr.triggered_by AS last_run_triggered_by,
       lr.started_at AS last_run_started_at, lr.duration_ms AS last_run_duration_ms
FROM projects p
LEFT JOIN workspaces w ON w.id = p.workspace_id
LEFT JOIN LATERAL (
    SELECT id, status, trigger_ref, triggered_by, started_at, duration_ms
    FROM pipeline_runs
    WHERE project_id = p.id
    ORDER BY started_at DESC
    LIMIT 1
) lr ON true
WHERE p.is_archived = false
  AND (cardinality(@workspaces::text[]) = 0 OR w.slug = ANY(@workspaces::text[]))
  AND (cardinality(@tags::text[]) = 0 OR p.tags && @tags::text[])
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

-- name: GetProjectOrgID :one
SELECT org_id FROM projects WHERE id = $1;

-- name: UpsertProject :one
-- Workspace placement (the CRD is authoritative): use the declared
-- spec.workspace slug if given, else infer from the repo owner (the part
-- before "/"), else fall back to the org's default "Unsorted" bucket. The
-- target workspace is created on the fly if it doesn't exist, and
-- workspace_inferred records whether placement was declared or inferred.
WITH fc AS (
    SELECT f.id, f.org_id FROM forge_connections f WHERE f.display_name = @forge_ref LIMIT 1
),
target AS (
    SELECT
        fc.id AS forge_id,
        fc.org_id,
        (NULLIF(@workspace::text, '') IS NULL) AS inferred,
        COALESCE(
            NULLIF(@workspace::text, ''),
            NULLIF(lower(split_part(@repo_path::text, '/', 1)), '')
        ) AS ws_slug
    FROM fc
),
ws AS (
    INSERT INTO workspaces (org_id, name, slug)
    SELECT org_id, ws_slug, ws_slug FROM target WHERE ws_slug IS NOT NULL
    ON CONFLICT (org_id, slug) DO UPDATE SET slug = EXCLUDED.slug
    RETURNING id
)
INSERT INTO projects (
    org_id, forge_id, repo_path, repo_url, display_name, description,
    colour, icon, tags, default_branch, pipeline_source, is_archived, updated_at,
    workspace_id, workspace_inferred
)
SELECT
    t.org_id, t.forge_id, @repo_path, @repo_url, @display_name, @description,
    @colour, @icon, @tags, @default_branch, @pipeline_source::jsonb, false, now(),
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
    tags = EXCLUDED.tags,
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
