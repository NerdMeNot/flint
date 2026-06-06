-- name: ListProjects :many
SELECT id, repo_path, display_name, description, colour, tags, default_branch, is_archived, created_at
FROM projects
WHERE is_archived = false
ORDER BY display_name, repo_path;

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
INSERT INTO projects (
    org_id, forge_id, repo_path, repo_url, display_name, description,
    colour, icon, tags, default_branch, pipeline_source, is_archived, updated_at
)
SELECT
    fc.org_id, fc.id, @repo_path, @repo_url, @display_name, @description,
    @colour, @icon, @tags, @default_branch, @pipeline_source::jsonb, false, now()
FROM forge_connections fc WHERE fc.display_name = @forge_ref
LIMIT 1
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
    updated_at = now()
RETURNING id;

-- name: GetProjectRepoInfo :one
SELECT repo_path, org_id, COALESCE(pipeline_source->>'path', '.flint/')::text AS pipeline_path
FROM projects WHERE id = $1;
