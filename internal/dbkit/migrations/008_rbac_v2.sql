-- +goose Up

-- ────────────────────────────────────────────────────────────
-- RBAC v2: Two-layer model (admin + CI), dual scope
-- (workspace + environment), permission implications,
-- role-carried scope, API key role-based permissions.
--
-- See docs/design/rbac.md for full design.
-- ────────────────────────────────────────────────────────────

-- Add updated_at to roles.
ALTER TABLE roles ADD COLUMN IF NOT EXISTS updated_at timestamptz NOT NULL DEFAULT now();

-- ────────────────────────────────────────────────────────────
-- Role permissions: explicit permission grants per role.
-- Both admin (read/manage) and CI (read/write/trigger/cancel/
-- approve/reject) stored in one table. Domain (admin vs CI)
-- determined by object name at runtime.
--
-- Only explicit permissions are stored. Implied permissions
-- (e.g., gate:approve implies run:read + project:read) are
-- computed at policy generation and access check time.
-- ────────────────────────────────────────────────────────────
CREATE TABLE role_permissions (
    role_id uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    object  text NOT NULL,
    action  text NOT NULL,
    PRIMARY KEY (role_id, object, action)
);

-- ────────────────────────────────────────────────────────────
-- Role scope: optional workspace + environment restrictions
-- for CI permissions. Empty = all. Admin permissions ignore
-- these scopes entirely.
-- ────────────────────────────────────────────────────────────
CREATE TABLE role_workspace_scope (
    role_id      uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, workspace_id)
);

CREATE TABLE role_environment_scope (
    role_id        uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    environment_id uuid NOT NULL REFERENCES protected_environments(id) ON DELETE CASCADE,
    PRIMARY KEY (role_id, environment_id)
);

-- ────────────────────────────────────────────────────────────
-- Role assignments: subject → role.
-- No workspace or environment in the assignment — the role
-- carries the scope. Subjects are user emails, team: prefixed
-- slugs, or apikey: prefixed IDs.
-- ────────────────────────────────────────────────────────────
CREATE TABLE role_assignments (
    subject    text NOT NULL,
    role_id    uuid NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (subject, role_id)
);

CREATE INDEX idx_role_assignments_role ON role_assignments(role_id);
CREATE INDEX idx_role_assignments_subject ON role_assignments(subject);

-- ────────────────────────────────────────────────────────────
-- API key: add role reference. Keys inherit permissions from
-- their assigned role, optionally with a narrower scope.
-- The old scopes column is kept for backward compatibility
-- but no longer used for authorization.
-- ────────────────────────────────────────────────────────────
ALTER TABLE api_keys ADD COLUMN role_id uuid REFERENCES roles(id);

CREATE TABLE api_key_workspace_scope (
    api_key_id   uuid NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    workspace_id uuid NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    PRIMARY KEY (api_key_id, workspace_id)
);

CREATE TABLE api_key_environment_scope (
    api_key_id     uuid NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    environment_id uuid NOT NULL REFERENCES protected_environments(id) ON DELETE CASCADE,
    PRIMARY KEY (api_key_id, environment_id)
);

-- ────────────────────────────────────────────────────────────
-- Data migration: seed new system roles with permissions.
-- The old Casbin rules will be truncated and regenerated
-- by the application on next boot.
-- ────────────────────────────────────────────────────────────

-- Map old system role slugs to new ones.
-- org_admin → admin, developer → developer, viewer → viewer.
-- pipeline_admin has no direct equivalent — kept as custom role.
UPDATE roles SET slug = 'admin', name = 'Admin',
    description = 'Full platform and CI access'
    WHERE slug = 'org_admin' AND is_system = true;

UPDATE roles SET name = 'Developer',
    description = 'Trigger and manage CI runs, read admin resources'
    WHERE slug = 'developer' AND is_system = true;

UPDATE roles SET name = 'Viewer',
    description = 'Read-only access across the platform'
    WHERE slug = 'viewer' AND is_system = true;

-- Convert pipeline_admin to custom role (no longer a system role).
UPDATE roles SET is_system = false,
    name = 'Pipeline Admin (Legacy)',
    description = 'Legacy role — migrated from pipeline_admin'
    WHERE slug = 'pipeline_admin' AND is_system = true;

-- Insert new Platform Manager system role (per-org).
-- This inserts one per org that has existing roles.
INSERT INTO roles (org_id, name, slug, description, is_system, created_at, updated_at)
SELECT DISTINCT org_id, 'Platform Manager', 'platform-manager',
    'Full admin access without CI write permissions', true, now(), now()
FROM roles
WHERE NOT EXISTS (
    SELECT 1 FROM roles r2 WHERE r2.org_id = roles.org_id AND r2.slug = 'platform-manager'
);

-- Seed permissions for Admin role (*:*).
INSERT INTO role_permissions (role_id, object, action)
SELECT id, '*', '*' FROM roles WHERE slug = 'admin' AND is_system = true
ON CONFLICT DO NOTHING;

-- Seed permissions for Developer role.
INSERT INTO role_permissions (role_id, object, action)
SELECT id, unnest.object, unnest.action
FROM roles,
LATERAL (VALUES
    ('environment', 'read'), ('runner', 'read'), ('secret', 'read'),
    ('project', 'read'), ('project', 'write'),
    ('run', 'read'), ('run', 'trigger'), ('run', 'cancel')
) AS unnest(object, action)
WHERE slug = 'developer' AND is_system = true
ON CONFLICT DO NOTHING;

-- Seed permissions for Viewer role.
INSERT INTO role_permissions (role_id, object, action)
SELECT id, unnest.object, unnest.action
FROM roles,
LATERAL (VALUES
    ('workspace', 'read'), ('team', 'read'), ('environment', 'read'), ('runner', 'read'),
    ('project', 'read'), ('run', 'read')
) AS unnest(object, action)
WHERE slug = 'viewer' AND is_system = true
ON CONFLICT DO NOTHING;

-- Seed permissions for Platform Manager role.
INSERT INTO role_permissions (role_id, object, action)
SELECT id, unnest.object, unnest.action
FROM roles,
LATERAL (VALUES
    ('workspace', 'read'), ('workspace', 'manage'),
    ('team', 'read'), ('team', 'manage'),
    ('environment', 'read'), ('environment', 'manage'),
    ('runner', 'read'), ('runner', 'manage'),
    ('connection', 'read'), ('connection', 'manage'),
    ('apikey', 'read'), ('apikey', 'manage'),
    ('secret', 'read'), ('secret', 'manage'),
    ('role', 'read'), ('role', 'manage'),
    ('audit', 'read'),
    ('project', 'read'), ('run', 'read')
) AS unnest(object, action)
WHERE slug = 'platform-manager' AND is_system = true
ON CONFLICT DO NOTHING;

-- Seed permissions for legacy pipeline_admin (custom role).
INSERT INTO role_permissions (role_id, object, action)
SELECT id, unnest.object, unnest.action
FROM roles,
LATERAL (VALUES
    ('secret', 'read'), ('secret', 'manage'),
    ('environment', 'read'),
    ('project', 'read'), ('project', 'write'),
    ('run', 'read'), ('run', 'trigger'), ('run', 'cancel'),
    ('gate', 'approve'), ('gate', 'reject')
) AS unnest(object, action)
WHERE slug = 'pipeline_admin'
ON CONFLICT DO NOTHING;

-- ────────────────────────────────────────────────────────────
-- Migrate existing Casbin grouping rules to role_assignments.
-- g rules: ptype='g', v0=subject, v1=role_slug, v2=workspace_or_*
-- New model: assignments are unscoped (role carries scope).
-- Workspace-specific assignments lose their scoping — known
-- limitation for the migration.
-- ────────────────────────────────────────────────────────────
INSERT INTO role_assignments (subject, role_id, created_at)
SELECT cr.v0, r.id, now()
FROM casbin_rules cr
JOIN roles r ON r.slug = cr.v1
WHERE cr.ptype = 'g'
ON CONFLICT DO NOTHING;

-- Clear all Casbin rules. The application will regenerate them
-- from role_assignments + role_permissions on next boot.
TRUNCATE casbin_rules;

-- +goose Down

TRUNCATE casbin_rules;

DROP TABLE IF EXISTS api_key_environment_scope;
DROP TABLE IF EXISTS api_key_workspace_scope;
ALTER TABLE api_keys DROP COLUMN IF EXISTS role_id;

DROP TABLE IF EXISTS role_assignments;
DROP TABLE IF EXISTS role_environment_scope;
DROP TABLE IF EXISTS role_workspace_scope;
DROP TABLE IF EXISTS role_permissions;

ALTER TABLE roles DROP COLUMN IF EXISTS updated_at;

-- Restore old role names (best-effort).
UPDATE roles SET slug = 'org_admin', name = 'Org Admin'
    WHERE slug = 'admin' AND is_system = true;
UPDATE roles SET is_system = true, slug = 'pipeline_admin', name = 'Pipeline Admin'
    WHERE slug = 'pipeline_admin';
