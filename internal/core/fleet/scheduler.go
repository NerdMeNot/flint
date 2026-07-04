package fleet

import (
	"context"
	"slices"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// claimDeadline bounds how long a bound assignment may sit unclaimed before
// the sweep releases it back to pending (the agent normally claims within one
// long-poll cycle).
const claimDeadline = 60 * time.Second

// schedulerBatch caps how many pending assignments one pool binds per pass.
const schedulerBatch = 50

// SchedulePending binds pending assignments to machines with free capacity:
// warmth first (a machine already holding this run's workspace and caches),
// then best-fit by CPU so large future requests keep a machine to land on.
// Assignments that fit no machine stay pending — boot-new decisions belong to
// the provisioner, not the scheduler. Returns the number bound.
func (f *Fleet) SchedulePending(ctx context.Context) (int, error) {
	q := db.New(f.pool)
	demand, err := q.PendingAssignmentDemand(ctx)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, pool := range demand {
		n, err := f.schedulePool(ctx, pool.PoolID)
		if err != nil {
			log.Error().Err(err).Str("pool", pool.PoolID).Msg("fleet: scheduling pass failed")
			continue
		}
		total += n
	}
	return total, nil
}

// candidate is a machine with mutable free-capacity bookkeeping for one pass.
type candidate struct {
	id        string
	status    string
	freeCPU   int64
	freeMem   int64
	runIDs    []string
	nowBusy   bool // became busy during this pass
	boundHere int
}

func (f *Fleet) schedulePool(ctx context.Context, poolID string) (int, error) {
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(f.pool).WithTx(tx)

	pending, err := qtx.ClaimPendingAssignments(ctx, db.ClaimPendingAssignmentsParams{
		PoolID: poolID, Limit: schedulerBatch,
	})
	if err != nil || len(pending) == 0 {
		return 0, err
	}

	rows, err := qtx.MachineFreeCapacity(ctx, poolID)
	if err != nil {
		return 0, err
	}
	candidates := make([]*candidate, 0, len(rows))
	for _, m := range rows {
		candidates = append(candidates, &candidate{
			id:     m.ID,
			status: m.Status,
			// Free = capacity − committed; capacity 0 means "unknown/unbounded"
			// (a machine that self-reported nothing) — treat as roomy.
			freeCPU: freeOrUnbounded(m.CpuMillis, m.CommittedCpuMillis),
			freeMem: freeOrUnbounded(m.MemoryMb, m.CommittedMemoryMb),
			runIDs:  m.ActiveRunIds,
		})
	}

	bound := 0
	deadline := time.Now().Add(claimDeadline)
	for _, a := range pending {
		best := pickMachine(candidates, a)
		if best == nil {
			continue // stays pending; the provisioner owns boot-new
		}
		if err := qtx.BindAssignment(ctx, db.BindAssignmentParams{
			ID: a.ID, MachineID: &best.id, ClaimDeadlineAt: &deadline,
		}); err != nil {
			return 0, err
		}
		// idle → busy on the machine's first live assignment.
		if best.status == machineIdle && !best.nowBusy {
			if err := transitionMachine(ctx, qtx, machineTransition{
				machineID: best.id, from: machineIdle, to: machineBusy,
				eventType: "assigned", actor: actorFleet,
				metadata: map[string]any{"assignment": a.ID, "step": a.StepName, "run": a.RunID},
			}); err != nil {
				return 0, err
			}
			best.nowBusy = true
		}
		best.freeCPU -= a.CpuMillis
		best.freeMem -= a.MemoryMb
		best.runIDs = append(best.runIDs, a.RunID)
		best.boundHere++
		bound++
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	if bound > 0 {
		log.Debug().Int("bound", bound).Str("pool", poolID).Msg("fleet: assignments scheduled")
	}
	return bound, nil
}

// pickMachine chooses the machine for one assignment: warmth (same run ⇒ same
// machine — its workspace and caches are already there), then best-fit
// (smallest sufficient free CPU).
func pickMachine(candidates []*candidate, a db.ClaimPendingAssignmentsRow) *candidate {
	var best *candidate
	bestWarm := false
	for _, c := range candidates {
		if c.freeCPU < a.CpuMillis || c.freeMem < a.MemoryMb {
			continue
		}
		warm := slices.Contains(c.runIDs, a.RunID)
		switch {
		case best == nil,
			warm && !bestWarm,
			warm == bestWarm && c.freeCPU < best.freeCPU:
			best = c
			bestWarm = warm
		}
	}
	return best
}

// ReleaseUnclaimed returns assigned-but-never-claimed assignments (agent died
// between bind and claim) to the pending queue.
func (f *Fleet) ReleaseUnclaimed(ctx context.Context) (int, error) {
	rows, err := db.New(f.pool).ReleaseUnclaimedAssignments(ctx)
	if err != nil {
		return 0, err
	}
	if len(rows) > 0 {
		log.Warn().Int("released", len(rows)).
			Msg("fleet: unclaimed assignments returned to pending")
	}
	return len(rows), nil
}

// freeOrUnbounded computes free capacity, treating an unreported capacity (0)
// as effectively unbounded so self-reporting-less machines still schedule.
func freeOrUnbounded(capacity, committed int64) int64 {
	if capacity <= 0 {
		return 1 << 40
	}
	return capacity - committed
}
