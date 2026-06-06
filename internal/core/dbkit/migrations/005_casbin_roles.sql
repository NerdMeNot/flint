-- +goose Up

-- ────────────────────────────────────────────────────────────
-- Casbin policy storage.
-- Used by the custom pgx adapter in internal/auth/casbin.go.
-- ptype "p" = policy rule, "g" = grouping (role assignment).
-- ────────────────────────────────────────────────────────────
CREATE TABLE casbin_rules (
    id     bigserial PRIMARY KEY,
    ptype  text NOT NULL,
    v0     text NOT NULL DEFAULT '',
    v1     text NOT NULL DEFAULT '',
    v2     text NOT NULL DEFAULT '',
    v3     text NOT NULL DEFAULT '',
    v4     text NOT NULL DEFAULT '',
    v5     text NOT NULL DEFAULT '',
    UNIQUE (ptype, v0, v1, v2, v3, v4, v5)
);

CREATE INDEX idx_casbin_rules_ptype ON casbin_rules(ptype);

-- ────────────────────────────────────────────────────────────
-- Role metadata. Permissions live in casbin_rules;
-- this table stores display info and tracks system vs custom.
-- ────────────────────────────────────────────────────────────
CREATE TABLE roles (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL REFERENCES orgs(id),
    name        text NOT NULL,
    slug        text NOT NULL,
    description text,
    is_system   bool NOT NULL DEFAULT false,
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, slug)
);

-- rbac_policies is replaced by casbin_rules.
DROP TABLE IF EXISTS rbac_policies;

-- +goose Down
-- Recreate rbac_policies (original from migration 001).
CREATE TABLE rbac_policies (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        uuid NOT NULL REFERENCES orgs(id),
    group_claim   text NOT NULL,
    role          text NOT NULL CHECK (role IN (
                      'org_admin', 'pipeline_admin', 'developer', 'viewer'
                  )),
    resource_type text,
    resource_id   text,
    created_at    timestamptz NOT NULL DEFAULT now()
);

DROP TABLE IF EXISTS roles;
DROP TABLE IF EXISTS casbin_rules;
