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

	if err := qtx.FinishAssignment(ctx, db.FinishAssignmentParams{
		ID: assignmentID, Status: status, Error: errMsg,
	}); err != nil {
		return err
	}
	if err := qtx.IncrementMachineStepsCompleted(ctx, machineID); err != nil {
		return err
	}

	active, err := qtx.ListActiveAssignmentsForMachine(ctx, &machineID)
	if err != nil {
		return err
	}
	if len(active) == 0 {
		m, err := qtx.GetMachine(ctx, machineID)
		if err == nil && m.Status == machineBusy {
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
