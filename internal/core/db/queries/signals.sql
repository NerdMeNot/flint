-- name: InsertSignal :exec
INSERT INTO signals (workflow_id, signal_name, payload) VALUES ($1, $2, $3);

-- name: ConsumeStepResultSignals :many
UPDATE signals SET consumed = true
WHERE workflow_id = $1 AND signal_name = 'step-result' AND consumed = false
RETURNING payload;

-- name: ConsumeSignal :exec
UPDATE signals SET consumed = true WHERE id = $1;

-- name: WorkflowsWithPendingStepSignals :many
-- Running workflows that have an unconsumed informer step-result signal. The
-- loop advances these promptly so a crashed agent's step is resolved by the
-- informer in seconds, not at the step-timeout sweep. Uses the
-- idx_signals_unconsumed partial index.
SELECT DISTINCT s.workflow_id
FROM signals s
JOIN workflows w ON w.id = s.workflow_id
WHERE s.consumed = false AND s.signal_name = 'step-result' AND w.status = 'running'
LIMIT 100;

-- name: DeleteConsumedSignals :exec
-- Prune consumed signals so the table doesn't grow unbounded. Keeps a 7-day
-- window for debugging/audit. Called from the sweep.
DELETE FROM signals WHERE consumed = true AND created_at < now() - interval '7 days';

-- name: DeleteStaleUnconsumedSignals :exec
-- Prune unconsumed signals that never matched anything (e.g. an informer
-- step-result for a workflow that finished first, or an external signal with
-- no waiting step). Any legitimate consumer has long since timed out at 7 days.
DELETE FROM signals WHERE consumed = false AND created_at < now() - interval '7 days';

-- name: NotifyEngine :exec
SELECT pg_notify('flint_engine', $1);
