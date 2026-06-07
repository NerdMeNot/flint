-- +goose Up
-- Cron-scheduled workflow runs. A schedule stores a workflow definition (YAML)
-- and a cron expression; the worker's scheduler fires due schedules, creating a
-- run per fire. Separate from the one-time consolidated baseline (001) as a
-- normal forward-feature migration.
CREATE TABLE workflow_schedules (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id      uuid NOT NULL REFERENCES orgs(id),
    name        text NOT NULL,
    cron        text NOT NULL,
    definition  text NOT NULL,
    enabled     boolean NOT NULL DEFAULT true,
    next_run_at timestamptz NOT NULL,
    last_run_at timestamptz,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_workflow_schedules_due ON workflow_schedules (next_run_at) WHERE enabled;

-- +goose Down
DROP TABLE workflow_schedules;
