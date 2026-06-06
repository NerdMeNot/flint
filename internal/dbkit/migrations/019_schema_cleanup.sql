-- +goose Up

-- Drop columns that are never set or read by any query.

-- pipeline_runs.runner_pool: created in 001, never populated by InsertPipelineRun/
-- InsertManualRun/InsertRetryRun, never selected by GetRun/ListRunsByProject.
ALTER TABLE pipeline_runs DROP COLUMN IF EXISTS runner_pool;

-- projects.webhook_id: superseded by the webhooks table (migration 016).
-- No query reads or writes this column.
ALTER TABLE projects DROP COLUMN IF EXISTS webhook_id;

-- +goose Down

ALTER TABLE pipeline_runs ADD COLUMN IF NOT EXISTS runner_pool text;
ALTER TABLE projects ADD COLUMN IF NOT EXISTS webhook_id text;
