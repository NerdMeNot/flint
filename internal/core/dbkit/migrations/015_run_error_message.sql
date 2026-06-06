-- +goose Up

-- Add error_message to pipeline_runs so users can see WHY a run failed
-- (e.g. YAML parse error, DAG cycle, forge fetch failure).
ALTER TABLE pipeline_runs ADD COLUMN IF NOT EXISTS error_message text;

-- FailRunWithError: used when StartWorkflow fails before any steps exist.
-- Separate from FinishRun which handles normal completion after step execution.

-- +goose Down

ALTER TABLE pipeline_runs DROP COLUMN IF EXISTS error_message;
