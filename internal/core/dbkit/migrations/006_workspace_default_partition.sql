-- +goose Up
-- Make workspace a strict partition: every project belongs to exactly one
-- workspace (no scopeless orphans). Each org has exactly one default workspace
-- that new projects land in when none is specified.

-- 1. Mark a default workspace per org.
ALTER TABLE workspaces ADD COLUMN is_default boolean NOT NULL DEFAULT false;

-- 2. Orgs with no workspace at all get a "Default" one.
INSERT INTO workspaces (org_id, name, slug, is_default)
SELECT o.id, 'Default', 'default', true
FROM orgs o
WHERE NOT EXISTS (SELECT 1 FROM workspaces w WHERE w.org_id = o.id);

-- 3. Orgs that have workspaces but none flagged default: flag the earliest one.
UPDATE workspaces w SET is_default = true
WHERE w.id = (
    SELECT w2.id FROM workspaces w2
    WHERE w2.org_id = w.org_id
    ORDER BY w2.created_at, w2.id
    LIMIT 1
)
AND NOT EXISTS (SELECT 1 FROM workspaces d WHERE d.org_id = w.org_id AND d.is_default);

-- 4. Exactly one default per org.
CREATE UNIQUE INDEX idx_workspaces_one_default_per_org ON workspaces (org_id) WHERE is_default;

-- 5. Adopt orphan projects into their org's default workspace.
UPDATE projects p SET workspace_id = (
    SELECT w.id FROM workspaces w WHERE w.org_id = p.org_id AND w.is_default LIMIT 1
)
WHERE p.workspace_id IS NULL;

-- 6. Enforce the partition.
ALTER TABLE projects ALTER COLUMN workspace_id SET NOT NULL;

-- +goose Down
ALTER TABLE projects ALTER COLUMN workspace_id DROP NOT NULL;
DROP INDEX IF EXISTS idx_workspaces_one_default_per_org;
ALTER TABLE workspaces DROP COLUMN is_default;
