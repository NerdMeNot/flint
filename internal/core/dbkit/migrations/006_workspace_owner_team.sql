-- +goose Up
-- A workspace (project container) is owned by a team (group of users). This makes
-- the ownership relationship explicit: Team ──owns──▶ Workspace. Nullable so
-- unowned workspaces remain valid; ON DELETE SET NULL so deleting a team doesn't
-- cascade-delete its workspaces.
ALTER TABLE workspaces ADD COLUMN owner_team_id uuid REFERENCES teams(id) ON DELETE SET NULL;

CREATE INDEX idx_workspaces_owner_team ON workspaces (owner_team_id) WHERE owner_team_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_workspaces_owner_team;
ALTER TABLE workspaces DROP COLUMN owner_team_id;
