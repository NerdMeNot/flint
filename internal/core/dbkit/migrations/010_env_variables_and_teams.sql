-- +goose Up

-- ────────────────────────────────────────────────────────────
-- Simplified environments table.
-- Environments are just named entities. Protection rules come
-- from RBAC (role scoping), not from the environment itself.
-- See docs/design/environments.md
-- ────────────────────────────────────────────────────────────
CREATE TABLE environments (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES orgs(id),
    name       text NOT NULL,
    slug       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, slug)
);

-- Seed from existing protected_environments.
INSERT INTO environments (id, org_id, name, slug, created_at)
SELECT id, org_id, name, lower(replace(name, ' ', '-')), created_at
FROM protected_environments
ON CONFLICT DO NOTHING;

-- ────────────────────────────────────────────────────────────
-- Migrate FK references from protected_environments to
-- environments for the RBAC scope tables.
-- ────────────────────────────────────────────────────────────

-- Drop and recreate role_environment_scope FK.
ALTER TABLE role_environment_scope
    DROP CONSTRAINT IF EXISTS role_environment_scope_environment_id_fkey;
ALTER TABLE role_environment_scope
    ADD CONSTRAINT role_environment_scope_environment_id_fkey
    FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE;

-- Drop and recreate api_key_environment_scope FK.
ALTER TABLE api_key_environment_scope
    DROP CONSTRAINT IF EXISTS api_key_environment_scope_environment_id_fkey;
ALTER TABLE api_key_environment_scope
    ADD CONSTRAINT api_key_environment_scope_environment_id_fkey
    FOREIGN KEY (environment_id) REFERENCES environments(id) ON DELETE CASCADE;

-- ────────────────────────────────────────────────────────────
-- Environment variables.
-- Variables are defined once and valued per environment.
-- scope = 'global' → single value, same everywhere
-- scope = 'environment' → different value per environment
-- See docs/design/environments.md
-- ────────────────────────────────────────────────────────────
CREATE TABLE env_variables (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL REFERENCES orgs(id),
    name        text NOT NULL,
    description text,
    scope       text NOT NULL DEFAULT 'environment'
                CHECK (scope IN ('global', 'environment')),
    is_secret   boolean NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

-- Per-environment values.
-- For global variables, environment_id IS NULL and the single
-- value is stored here (or on the variable row itself).
CREATE TABLE env_variable_values (
    variable_id    uuid NOT NULL REFERENCES env_variables(id) ON DELETE CASCADE,
    environment_id uuid REFERENCES environments(id) ON DELETE CASCADE,
    value          text NOT NULL,
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (variable_id, environment_id)
);

-- Index for quick lookup by environment.
CREATE INDEX idx_env_variable_values_env ON env_variable_values(environment_id);

-- ────────────────────────────────────────────────────────────
-- Migrate existing secrets to env_variables.
-- Org-level secrets become global env variables.
-- Environment-scoped secrets become environment-scoped variables.
-- ────────────────────────────────────────────────────────────
INSERT INTO env_variables (org_id, name, description, scope, is_secret, created_at)
SELECT DISTINCT ON (org_id, name)
    org_id, name, NULL,
    CASE WHEN environment IS NULL THEN 'global' ELSE 'environment' END,
    true, created_at
FROM secrets
ON CONFLICT DO NOTHING;

-- Migrate org-level secret values (environment IS NULL).
INSERT INTO env_variable_values (variable_id, environment_id, value, updated_at)
SELECT ev.id, NULL, s.encrypted_value, s.updated_at
FROM secrets s
JOIN env_variables ev ON ev.org_id = s.org_id AND ev.name = s.name
WHERE s.environment IS NULL AND s.encrypted_value IS NOT NULL
ON CONFLICT DO NOTHING;

-- Migrate environment-scoped secret values.
INSERT INTO env_variable_values (variable_id, environment_id, value, updated_at)
SELECT ev.id, e.id, s.encrypted_value, s.updated_at
FROM secrets s
JOIN env_variables ev ON ev.org_id = s.org_id AND ev.name = s.name
JOIN environments e ON e.org_id = s.org_id AND e.name = s.environment
WHERE s.environment IS NOT NULL AND s.encrypted_value IS NOT NULL
ON CONFLICT DO NOTHING;

-- ────────────────────────────────────────────────────────────
-- Team source tracking.
-- source = 'idp' → synced from identity provider groups
-- source = 'internal' → created manually in Flint
-- idp_group → the IdP group name (null for internal teams)
-- ────────────────────────────────────────────────────────────
ALTER TABLE teams ADD COLUMN IF NOT EXISTS source text NOT NULL DEFAULT 'internal';
ALTER TABLE teams ADD COLUMN IF NOT EXISTS idp_group text;

-- Mark existing teams that were synced from IdP.
-- Teams created via syncTeams in auth/sync.go won't have a
-- marker, but we can infer: if a team's slug matches a
-- Casbin grouping policy target, it was likely IdP-synced.
-- For safety, we leave all as 'internal' — SyncUserOnLogin
-- will update the source on next login.

-- +goose Down

ALTER TABLE teams DROP COLUMN IF EXISTS idp_group;
ALTER TABLE teams DROP COLUMN IF EXISTS source;

DROP TABLE IF EXISTS env_variable_values;
DROP TABLE IF EXISTS env_variables;

-- Restore FK references to protected_environments.
ALTER TABLE role_environment_scope
    DROP CONSTRAINT IF EXISTS role_environment_scope_environment_id_fkey;
ALTER TABLE role_environment_scope
    ADD CONSTRAINT role_environment_scope_environment_id_fkey
    FOREIGN KEY (environment_id) REFERENCES protected_environments(id) ON DELETE CASCADE;

ALTER TABLE api_key_environment_scope
    DROP CONSTRAINT IF EXISTS api_key_environment_scope_environment_id_fkey;
ALTER TABLE api_key_environment_scope
    ADD CONSTRAINT api_key_environment_scope_environment_id_fkey
    FOREIGN KEY (environment_id) REFERENCES protected_environments(id) ON DELETE CASCADE;

DROP TABLE IF EXISTS environments;
