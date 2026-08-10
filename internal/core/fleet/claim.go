package fleet

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// ClaimForMachine atomically hands the oldest assigned work on this machine to
// the agent (assignment → running). Returns nil when nothing is assigned —
// the gRPC layer owns the long-poll parking around this.
func (f *Fleet) ClaimForMachine(ctx context.Context, machineID string) (*db.ClaimAssignmentForAgentRow, error) {
	row, err := db.New(f.pool).ClaimAssignmentForAgent(ctx, &machineID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

// CompleteAssignment finalizes an assignment after the engine accepted the
// step result: assignment → terminal, machine stats bumped, and busy → idle
// when this was the machine's last active assignment.
func (f *Fleet) CompleteAssignment(ctx context.Context, assignmentID, machineID, status string, errMsg *string) error {
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(f.pool).WithTx(tx)

	n, err := qtx.FinishAssignment(ctx, db.FinishAssignmentParams{
		ID: assignmentID, Status: status, Error: errMsg,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		// Duplicate completion: this assignment was already finalized by an
		// earlier delivery. Bumping steps_completed or re-running the busy→idle
		// transition here would double-count and corrupt accounting, so no-op.
		return tx.Commit(ctx)
	}
	if err := qtx.IncrementMachineStepsCompleted(ctx, machineID); err != nil {
		return err
	}

	active, err := qtx.ListActiveAssignmentsForMachine(ctx, &machineID)
	if err != nil {
		return err
	}
	if len(active) == 0 {
		// Returned, not skipped on error. This is the ONLY path from busy → idle,
		// and a machine that misses it stays busy forever — scale-down only
		// considers idle machines, and a healthy agent keeps the lease renewed so
		// the heartbeat sweep never sees it either. Losing the transition to a
		// discarded read error meant a machine billing indefinitely. (The fleet
		// loop's busy-drift sweep is the backstop; this keeps the common path
		// honest so the backstop stays a backstop.)
		m, err := qtx.GetMachine(ctx, machineID)
		if err != nil {
			return err
		}
		if m.Status == machineBusy {
			if err := transitionMachine(ctx, qtx, machineTransition{
				machineID: machineID, from: machineBusy, to: machineIdle,
				eventType: "idle", actor: actorAgent,
			}); err != nil && !errors.Is(err, ErrMachineTransitionRaceLost) {
				// A lost race means another actor already moved the machine off
				// busy (scale-down, reconcile) — the assignment finalize must
				// still succeed, so swallow it.
				return err
			}
		}
	}
	return tx.Commit(ctx)
}
