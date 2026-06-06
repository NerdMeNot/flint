-- +goose Up

-- Performance indexes on columns frequently filtered in WHERE/JOIN clauses.
-- All use IF NOT EXISTS for idempotency.

CREATE INDEX IF NOT EXISTS idx_api_keys_org ON api_keys (org_id);
CREATE INDEX IF NOT EXISTS idx_projects_org ON projects (org_id);
CREATE INDEX IF NOT EXISTS idx_projects_workspace ON projects (workspace_id) WHERE workspace_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_org ON users (org_id);
CREATE INDEX IF NOT EXISTS idx_users_email ON users (email);
CREATE INDEX IF NOT EXISTS idx_sessions_user_expires ON sessions (user_id, expires_at);
CREATE INDEX IF NOT EXISTS idx_teams_org ON teams (org_id);
CREATE INDEX IF NOT EXISTS idx_workspaces_org ON workspaces (org_id);
CREATE INDEX IF NOT EXISTS idx_roles_org ON roles (org_id);
CREATE INDEX IF NOT EXISTS idx_role_assignments_subject_role ON role_assignments (subject, role_id);

-- +goose Down

DROP INDEX IF EXISTS idx_api_keys_org;
DROP INDEX IF EXISTS idx_projects_org;
DROP INDEX IF EXISTS idx_projects_workspace;
DROP INDEX IF EXISTS idx_users_org;
DROP INDEX IF EXISTS idx_users_email;
DROP INDEX IF EXISTS idx_sessions_user_expires;
DROP INDEX IF EXISTS idx_teams_org;
DROP INDEX IF EXISTS idx_workspaces_org;
DROP INDEX IF EXISTS idx_roles_org;
DROP INDEX IF EXISTS idx_role_assignments_subject_role;
