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
SELECT id, COALESCE(display_name, repo_path)::text AS name, repo_path, colour
FROM projects
WHERE org_id = sqlc.arg('org_id') AND is_archived = false
  AND (display_name ILIKE sqlc.arg('pattern') OR repo_path ILIKE sqlc.arg('pattern'))
ORDER BY display_name
LIMIT 10;

-- name: GetProjectBasic :one
-- Single project with workspace slug. The most recent run is fetched separately
-- (ListRunsByProject with limit 1) to keep nullability clean.
SELECT p.id, COALESCE(p.display_name, p.repo_path)::text AS name, p.repo_path,
       COALESCE(w.slug, '')::text AS workspace, p.colour, p.tags, p.created_at
FROM projects p
LEFT JOIN workspaces w ON w.id = p.workspace_id
WHERE p.id = $1;
