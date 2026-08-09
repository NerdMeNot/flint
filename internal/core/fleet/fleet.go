package fleet

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/engine"
)

// Fleet owns machine lifecycle operations: registration bookkeeping, heartbeat
// leases, deadline sweeps, and (via the scheduler/provisioner) placement and
// capacity decisions.
type Fleet struct {
	pool *pgxpool.Pool
	eng  engine.Engine
	providerCache
}

// New builds a Fleet over the shared Postgres pool. eng is used to route
// machine-death step failures through the engine's existing retry path.
func New(pool *pgxpool.Pool, eng engine.Engine) *Fleet {
	return &Fleet{pool: pool, eng: eng}
}

// Pool exposes the underlying connection pool for adapters (agentgrpc) that
// need read access beyond the Fleet's own operations.
func (f *Fleet) Pool() *pgxpool.Pool { return f.pool }

// ExpireBootDeadlines fails machines that never registered before their boot
// deadline (SKIP LOCKED — safe under concurrent sweepers). The provision
// decision's outcome is backfilled so the ledger records the boot timeout.
// Destroy of the half-created instance is reconciliation's job.
func (f *Fleet) ExpireBootDeadlines(ctx context.Context) (int, error) {
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(f.pool).WithTx(tx)

	rows, err := qtx.ClaimExpiredBootDeadlines(ctx)
	if err != nil {
		return 0, err
	}
	for _, m := range rows {
		if err := transitionMachine(ctx, qtx, machineTransition{
			machineID: m.ID, from: m.Status, to: machineFailed,
			eventType: "boot_timeout", actor: actorSweep,
			reason: "agent did not register before the boot deadline",
		}); err != nil {
			return 0, err
		}
		if err := qtx.ResolveFleetDecisionByMachine(ctx, db.ResolveFleetDecisionByMachineParams{
			MachineID: &m.ID, DecisionType: "provision", Outcome: strp("boot_timeout"),
		}); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	recordMachineLost(ctx, "boot_timeout", len(rows))
	if len(rows) > 0 {
		log.Warn().Int("machines", len(rows)).Msg("fleet: boot deadlines expired")
	}
	return len(rows), nil
}

// ExpireHeartbeatLeases marks machines whose heartbeat lease lapsed as lost
// and fails their live assignments; each failure is delivered to the engine as
// a step-result signal, so the existing retry policy takes over exactly as it
// did for crashed pods. Returns the number of machines lost.
func (f *Fleet) ExpireHeartbeatLeases(ctx context.Context) (int, error) {
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(f.pool).WithTx(tx)

	rows, err := qtx.ClaimExpiredHeartbeats(ctx)
	if err != nil {
		return 0, err
	}
	var failed []db.FailMachineAssignmentsRow
	for _, m := range rows {
		if err := transitionMachine(ctx, qtx, machineTransition{
			machineID: m.ID, from: m.Status, to: machineLost,
			eventType: "heartbeat_expired", actor: actorSweep,
			reason: "heartbeat lease expired",
		}); err != nil {
			return 0, err
		}
		errMsg := fmt.Sprintf("machine %s lost (heartbeat lease expired)", m.ID)
		af, err := qtx.FailMachineAssignments(ctx, db.FailMachineAssignmentsParams{
			MachineID: &m.ID, Error: &errMsg,
		})
		if err != nil {
			return 0, err
		}
		failed = append(failed, af...)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}

	// Signal delivery is post-commit and best-effort: a crash here leaves the
	// step to the engine's stale-running sweep (deadline_at), the same
	// guarantee the informer path had.
	f.failStepsViaSignals(ctx, failed)

	recordMachineLost(ctx, "heartbeat_expired", len(rows))
	if len(rows) > 0 {
		log.Warn().Int("machines", len(rows)).Int("assignments", len(failed)).
			Msg("fleet: heartbeat leases expired — machines lost")
	}
	return len(rows), nil
}

// failStepsViaSignals reports assignment deaths to the engine as step-result
// signals — the same seam the k8s informer used, so engine retry semantics
// apply unchanged.
func (f *Fleet) failStepsViaSignals(ctx context.Context, failed []db.FailMachineAssignmentsRow) {
	for _, a := range failed {
		payload := map[string]any{
			"stepName": a.StepName,
			"runID":    a.RunID,
			"success":  false,
			"reason":   "machine lost",
		}
		if err := f.eng.DeliverSignal(ctx, a.WorkflowID, "step-result", payload); err != nil {
			log.Error().Err(err).Str("workflow", a.WorkflowID).Str("step", a.StepName).
				Msg("fleet: failed to deliver machine-lost step-result signal")
		}
	}
}

func strp(s string) *string { return &s }
