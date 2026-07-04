-- name: InsertFleetDecision :one
-- Written in the same tx as the action it records (e.g. the machines insert for a
-- provision decision) so ledger and state can never disagree.
INSERT INTO fleet_decisions (pool_id, machine_id, decision_type, inputs, chosen, alternatives)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: ResolveFleetDecisionByMachine :exec
-- Backfills the outcome once known (boot_ok + boot seconds at registration,
-- boot_timeout from the sweep, terminated from scale-down/reconcile).
UPDATE fleet_decisions SET
    outcome = @outcome, outcome_metadata = @outcome_metadata, outcome_at = now()
WHERE machine_id = @machine_id AND decision_type = @decision_type AND outcome IS NULL;

-- name: ListFleetDecisions :many
SELECT * FROM fleet_decisions
WHERE (sqlc.narg(pool_id)::uuid IS NULL OR pool_id = sqlc.narg(pool_id))
  AND (sqlc.narg(decision_type)::text IS NULL OR decision_type = sqlc.narg(decision_type))
ORDER BY created_at DESC LIMIT $1 OFFSET $2;

-- name: LastNoCapacityDecision :one
-- Debounce input: no_capacity is recorded at most once per pool per window.
SELECT created_at FROM fleet_decisions
WHERE pool_id = $1 AND decision_type = 'no_capacity'
ORDER BY created_at DESC LIMIT 1;
