-- +goose Up

-- ────────────────────────────────────────────────────────────
-- Protected environments: deployment gates separate from RBAC.
-- Controls who can deploy where, with branch restrictions
-- and deploy windows.
-- ────────────────────────────────────────────────────────────
CREATE TABLE protected_environments (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          uuid NOT NULL REFERENCES orgs(id),
    name            text NOT NULL,
    min_role        text NOT NULL DEFAULT 'pipeline_admin',
    approvers       text[] NOT NULL DEFAULT '{}',
    deploy_branches text[] NOT NULL DEFAULT '{}',
    deploy_window   jsonb,
    created_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, name)
);

-- Environment-scoped secrets: only injected into steps
-- referencing this environment.
ALTER TABLE secrets ADD COLUMN environment text;

-- +goose Down
ALTER TABLE secrets DROP COLUMN IF EXISTS environment;
DROP TABLE IF EXISTS protected_environments;
