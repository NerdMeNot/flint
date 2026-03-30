-- +goose Up

-- ────────────────────────────────────────────────────────────
-- Workspaces: flat grouping of projects for RBAC scoping.
-- Each workspace is a Casbin "domain" — users can have
-- different roles per workspace.
-- ────────────────────────────────────────────────────────────
CREATE TABLE workspaces (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL REFERENCES orgs(id),
    name        text NOT NULL,
    slug        text NOT NULL,
    description text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, slug)
);

-- ────────────────────────────────────────────────────────────
-- Team membership: links users to teams.
-- Teams are synced from IdP groups on login.
-- ────────────────────────────────────────────────────────────
CREATE TABLE team_members (
    team_id uuid NOT NULL REFERENCES teams(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (team_id, user_id)
);

-- Replace team_id with workspace_id on projects.
ALTER TABLE projects ADD COLUMN workspace_id uuid REFERENCES workspaces(id);
ALTER TABLE projects DROP COLUMN IF EXISTS team_id;

-- +goose Down
ALTER TABLE projects ADD COLUMN team_id uuid REFERENCES teams(id);
ALTER TABLE projects DROP COLUMN IF EXISTS workspace_id;
DROP TABLE IF EXISTS team_members;
DROP TABLE IF EXISTS workspaces;
