-- name: InsertSignal :exec
INSERT INTO signals (workflow_id, signal_name, payload) VALUES ($1, $2, $3);

-- name: ConsumeStepResultSignals :many
UPDATE signals SET consumed = true
WHERE workflow_id = $1 AND signal_name = 'step-result' AND consumed = false
RETURNING payload;

-- name: ConsumeSignal :exec
UPDATE signals SET consumed = true WHERE id = $1;

-- name: NotifyEngine :exec
SELECT pg_notify('flint_engine', $1);
