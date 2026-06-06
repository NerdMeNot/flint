-- +goose Up

CREATE TABLE webhooks (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id  uuid NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    url         text NOT NULL,
    secret      text NOT NULL DEFAULT '',
    events      jsonb NOT NULL DEFAULT '["run.completed"]',
    is_active   boolean NOT NULL DEFAULT true,
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_webhooks_project ON webhooks (project_id) WHERE is_active = true;

-- +goose Down

DROP TABLE IF EXISTS webhooks;
