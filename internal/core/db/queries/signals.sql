-- name: InsertSignal :exec
INSERT INTO signals (workflow_id, signal_name, payload) VALUES ($1, $2, $3);

-- name: ConsumeStepResultSignals :many
UPDATE signals SET consumed = true
WHERE workflow_id = $1 AND signal_name = 'step-result' AND consumed = false
RETURNING payload;

-- name: ConsumeSignal :exec
UPDATE signals SET consumed = true WHERE id = $1;

-- name: DeleteConsumedSignals :exec
-- Prune consumed signals so the table doesn't grow unbounded. Keeps a 7-day
-- window for debugging/audit. Called from the sweep.
DELETE FROM signals WHERE consumed = true AND created_at < now() - interval '7 days';

-- name: NotifyEngine :exec
SELECT pg_notify('flint_engine', $1);
