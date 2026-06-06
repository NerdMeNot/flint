-- +goose Up

-- ────────────────────────────────────────────────────────────
-- Fix steps.exec_type to match pipeline parser output
-- Parser produces: 'run', 'use', 'steps', 'gate'
-- Remove stale values: 'do', 'invoke', 'watch', 'matrix'
-- ────────────────────────────────────────────────────────────
UPDATE steps SET exec_type = 'run'
WHERE exec_type IN ('do', 'invoke', 'watch', 'matrix');

ALTER TABLE steps DROP CONSTRAINT steps_exec_type_check;
ALTER TABLE steps ADD CONSTRAINT steps_exec_type_check
    CHECK (exec_type IN ('run', 'use', 'steps', 'gate'));

-- ────────────────────────────────────────────────────────────
-- Add environment to pipeline_runs
-- ────────────────────────────────────────────────────────────
ALTER TABLE pipeline_runs ADD COLUMN IF NOT EXISTS environment text;
CREATE INDEX IF NOT EXISTS idx_pipeline_runs_environment
    ON pipeline_runs (environment) WHERE environment IS NOT NULL;

-- ────────────────────────────────────────────────────────────
-- Drop legacy Temporal columns
-- ────────────────────────────────────────────────────────────
ALTER TABLE pipeline_runs DROP COLUMN IF EXISTS temporal_workflow_id;
ALTER TABLE orgs DROP COLUMN IF EXISTS temporal_namespace;

-- ────────────────────────────────────────────────────────────
-- Clean up timer_type constraint (remove dead watch types)
-- ────────────────────────────────────────────────────────────
DELETE FROM timers WHERE timer_type IN ('watch_interval', 'watch_timeout');

ALTER TABLE timers DROP CONSTRAINT timers_timer_type_check;
ALTER TABLE timers ADD CONSTRAINT timers_timer_type_check
    CHECK (timer_type IN ('timeout', 'gate_timeout', 'retry_backoff'));


-- +goose Down

-- Restore timer_type constraint
ALTER TABLE timers DROP CONSTRAINT timers_timer_type_check;
ALTER TABLE timers ADD CONSTRAINT timers_timer_type_check
    CHECK (timer_type IN ('timeout', 'gate_timeout', 'watch_interval',
                          'watch_timeout', 'retry_backoff'));

-- Restore Temporal columns
ALTER TABLE orgs ADD COLUMN IF NOT EXISTS temporal_namespace text NOT NULL DEFAULT 'default';
ALTER TABLE pipeline_runs ADD COLUMN IF NOT EXISTS temporal_workflow_id text;

-- Drop environment
DROP INDEX IF EXISTS idx_pipeline_runs_environment;
ALTER TABLE pipeline_runs DROP COLUMN IF EXISTS environment;

-- Restore steps exec_type constraint
ALTER TABLE steps DROP CONSTRAINT steps_exec_type_check;
ALTER TABLE steps ADD CONSTRAINT steps_exec_type_check
    CHECK (exec_type IN ('run', 'do', 'use', 'invoke', 'gate', 'watch', 'matrix'));
