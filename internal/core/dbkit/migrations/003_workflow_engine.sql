-- +goose Up

-- ────────────────────────────────────────────────────────────
-- Workflows
-- ────────────────────────────────────────────────────────────
CREATE TABLE workflows (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id           uuid NOT NULL REFERENCES pipeline_runs(id) ON DELETE CASCADE,
    parent_id        uuid REFERENCES workflows(id) ON DELETE CASCADE,
    parent_step      text,
    status           text NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending','running','succeeded','failed','cancelled')),
    input            jsonb NOT NULL,
    output           jsonb,
    pipeline_yaml    bytea,
    pipeline_def     jsonb,
    dag_waves        jsonb,
    step_outputs     jsonb NOT NULL DEFAULT '{}',
    created_at       timestamptz NOT NULL DEFAULT now(),
    started_at       timestamptz,
    finished_at      timestamptz,
    cancelled_at     timestamptz
);

-- Prevent duplicate root workflows per run (child workflows can share run_id).
CREATE UNIQUE INDEX idx_workflows_unique_root ON workflows (run_id) WHERE parent_id IS NULL;

CREATE INDEX idx_workflows_run_id ON workflows (run_id);
CREATE INDEX idx_workflows_parent ON workflows (parent_id) WHERE parent_id IS NOT NULL;
CREATE INDEX idx_workflows_active ON workflows (status) WHERE status IN ('pending', 'running');

-- ────────────────────────────────────────────────────────────
-- Steps
-- ────────────────────────────────────────────────────────────
CREATE TABLE steps (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id      uuid NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    name             text NOT NULL CHECK (length(name) > 0),
    exec_type        text NOT NULL CHECK (exec_type IN ('run','do','use','invoke','gate','watch','matrix')),
    status           text NOT NULL DEFAULT 'pending'
                     CHECK (status IN ('pending','queued','running','succeeded','failed',
                                       'skipped','cancelled','waiting')),
    wave             int NOT NULL,
    attempt          int NOT NULL DEFAULT 0,
    max_attempts     int NOT NULL DEFAULT 1,
    step_def         jsonb NOT NULL,
    result           jsonb,
    k8s_job_name     text,
    task_token       text,
    on_failure       text NOT NULL DEFAULT 'fail',

    timeout_seconds  int NOT NULL DEFAULT 7200,
    retry_backoff    text NOT NULL DEFAULT 'exponential',
    retry_interval_seconds int NOT NULL DEFAULT 5,

    created_at       timestamptz NOT NULL DEFAULT now(),
    queued_at        timestamptz,
    started_at       timestamptz,
    finished_at      timestamptz,
    deadline_at      timestamptz,

    UNIQUE (workflow_id, name, attempt)
);

CREATE INDEX idx_steps_workflow_wave ON steps (workflow_id, wave, status);
CREATE INDEX idx_steps_queued ON steps (status, queued_at) WHERE status = 'queued';
CREATE INDEX idx_steps_running ON steps (status, deadline_at) WHERE status = 'running';
CREATE INDEX idx_steps_waiting ON steps (status) WHERE status = 'waiting';
CREATE INDEX idx_steps_latest_attempt ON steps (workflow_id, name, attempt DESC);

-- ────────────────────────────────────────────────────────────
-- Timers
-- ────────────────────────────────────────────────────────────
CREATE TABLE timers (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id      uuid NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    step_name        text NOT NULL,
    timer_type       text NOT NULL CHECK (timer_type IN (
                         'timeout','gate_timeout','watch_interval',
                         'watch_timeout','retry_backoff')),
    fires_at         timestamptz NOT NULL,
    fired            boolean NOT NULL DEFAULT false,
    created_at       timestamptz NOT NULL DEFAULT now(),

    UNIQUE (workflow_id, step_name, timer_type)
);

CREATE INDEX idx_timers_pending ON timers (fires_at) WHERE fired = false;

-- ────────────────────────────────────────────────────────────
-- Signals
-- ────────────────────────────────────────────────────────────
CREATE TABLE signals (
    id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id      uuid NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    signal_name      text NOT NULL,
    payload          jsonb NOT NULL,
    consumed         boolean NOT NULL DEFAULT false,
    created_at       timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX idx_signals_unconsumed ON signals (workflow_id, signal_name)
    WHERE consumed = false;

-- ────────────────────────────────────────────────────────────
-- Add workflow_id + search columns to pipeline_runs
-- ────────────────────────────────────────────────────────────
ALTER TABLE pipeline_runs
    ADD COLUMN IF NOT EXISTS workflow_id uuid,
    ADD COLUMN IF NOT EXISTS branch text,
    ADD COLUMN IF NOT EXISTS repo text;

CREATE INDEX IF NOT EXISTS idx_pipeline_runs_branch ON pipeline_runs (branch) WHERE branch IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_pipeline_runs_repo ON pipeline_runs (repo) WHERE repo IS NOT NULL;

ALTER TABLE pipeline_runs ALTER COLUMN temporal_workflow_id DROP NOT NULL;


-- +goose Down

ALTER TABLE pipeline_runs ALTER COLUMN temporal_workflow_id SET NOT NULL;
DROP INDEX IF EXISTS idx_pipeline_runs_repo;
DROP INDEX IF EXISTS idx_pipeline_runs_branch;
ALTER TABLE pipeline_runs DROP COLUMN IF EXISTS repo;
ALTER TABLE pipeline_runs DROP COLUMN IF EXISTS branch;
ALTER TABLE pipeline_runs DROP COLUMN IF EXISTS workflow_id;
DROP TABLE IF EXISTS signals;
DROP TABLE IF EXISTS timers;
DROP TABLE IF EXISTS steps;
DROP TABLE IF EXISTS workflows;
