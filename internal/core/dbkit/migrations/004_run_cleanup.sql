-- +goose Up
-- Track per-run executor cleanup (workspace pod + leftover Jobs) so the worker
-- can tear down resources exactly once, on the first tick after a run reaches a
-- terminal state — for ALL terminal transitions (succeeded, failed, cancelled),
-- not just the 10-minute sweep window. cleaned_at NULL means "not yet cleaned".
ALTER TABLE pipeline_runs ADD COLUMN cleaned_at timestamptz;

CREATE INDEX idx_pipeline_runs_needs_cleanup
    ON pipeline_runs (id)
    WHERE cleaned_at IS NULL AND status IN ('succeeded', 'failed', 'cancelled');

-- +goose Down
DROP INDEX IF EXISTS idx_pipeline_runs_needs_cleanup;
ALTER TABLE pipeline_runs DROP COLUMN cleaned_at;
