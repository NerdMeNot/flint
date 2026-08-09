package fleet

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/pkg/compute"
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
	id            string
	status        string
	freeCPU       int64
	freeMem       int64
	interruptible bool // reclaimable (spot) capacity — avoided for new run holders
	nowBusy       bool // became busy during this pass
	boundHere     int
}

func (f *Fleet) schedulePool(ctx context.Context, poolID string) (int, error) {
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(f.pool).WithTx(tx)

	// Serialize a pool's scheduling across workers. Without it, two replicas read
	// the same MachineFreeCapacity snapshot (neither's binds committed yet) and
	// both pack their disjoint pending sets onto the same machine, overcommitting
	// its CPU/mem. A transaction-scoped advisory lock (auto-released on commit)
	// makes the loser wait, then read the winner's committed binds. Fast path —
	// scheduling is pure DB, no external calls — so blocking here is cheap.
	if _, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtext('flint-sched:' || $1))`, poolID); err != nil {
		return 0, err
	}

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
	byID := make(map[string]*candidate, len(rows))
	for _, m := range rows {
		c := &candidate{
			id:     m.ID,
			status: m.Status,
			// Free = capacity − committed; capacity 0 means "unknown/unbounded"
			// (a machine that self-reported nothing) — treat as roomy.
			freeCPU:       freeOrUnbounded(m.CpuMillis, m.CommittedCpuMillis),
			freeMem:       freeOrUnbounded(m.MemoryMb, m.CommittedMemoryMb),
			interruptible: m.CapacityType != nil && compute.CapacityType(*m.CapacityType).Interruptible(),
		}
		candidates = append(candidates, c)
		byID[m.ID] = c
	}

	// Resolve workspace holders for just this batch's runs. Asking per-run keeps
	// the cost proportional to the batch rather than to every assignment the
	// pool's machines have ever run.
	holders, err := f.runHolders(ctx, qtx, poolID, pending)
	if err != nil {
		return 0, err
	}

	bound := 0
	var queueWaits []time.Duration
	deadline := time.Now().Add(claimDeadline)
	for _, a := range pending {
		best := pickMachine(candidates, byID[holders[a.RunID]], a)
		if best == nil {
			continue // stays pending; the provisioner owns boot-new
		}
		// This binding establishes (or confirms) the run's holder, so later
		// assignments for the same run in THIS batch follow it — the in-pass
		// equivalent of what RunAffinityHolders will return on the next pass.
		holders[a.RunID] = best.id
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
		best.boundHere++
		bound++
		queueWaits = append(queueWaits, time.Since(a.CreatedAt))
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	// Reported only after the commit: a binding that rolled back never happened,
	// and a queue-wait histogram that counts phantom placements is worse than none.
	for _, w := range queueWaits {
		recordAssignmentBound(ctx, poolID, w)
	}
	if bound > 0 {
		log.Debug().Int("bound", bound).Str("pool", poolID).Msg("fleet: assignments scheduled")
	}
	return bound, nil
}

// runHolders maps run id → the machine already holding that run's workspace,
// for the runs present in this batch. Runs with no holder are simply absent.
func (f *Fleet) runHolders(ctx context.Context, qtx *db.Queries, poolID string, pending []db.ClaimPendingAssignmentsRow) (map[string]string, error) {
	seen := make(map[string]struct{}, len(pending))
	runIDs := make([]string, 0, len(pending))
	for _, a := range pending {
		if _, ok := seen[a.RunID]; ok {
			continue
		}
		seen[a.RunID] = struct{}{}
		runIDs = append(runIDs, a.RunID)
	}
	rows, err := qtx.RunAffinityHolders(ctx, db.RunAffinityHoldersParams{
		RunIds: runIDs, PoolID: poolID,
	})
	if err != nil {
		return nil, err
	}
	holders := make(map[string]string, len(rows))
	for _, r := range rows {
		if r.MachineID != nil {
			holders[r.RunID] = *r.MachineID
		}
	}
	return holders, nil
}

// pickMachine chooses the machine for one assignment. Run affinity is a
// GUARANTEE, not a preference: a run's workspace is a local directory on the
// machine that started it, so every later step of that run must land there.
// If the holder currently lacks capacity the assignment stays pending until
// it frees up (workspace correctness beats intra-run parallelism); if the
// holder died, its assignments already failed through the machine-lost path
// and the retry starts fresh. Runs with no holder yet get best-fit (smallest
// sufficient free CPU, so large future requests keep a machine to land on).
//
// holder is the run's established workspace machine, or nil if it has none
// (including the case where the holder is no longer a schedulable candidate —
// a dead machine unpins its runs so retries can start somewhere new).
func pickMachine(candidates []*candidate, holder *candidate, a db.ClaimPendingAssignmentsRow) *candidate {
	if holder != nil {
		if holder.freeCPU >= a.CpuMillis && holder.freeMem >= a.MemoryMb {
			return holder
		}
		return nil // wait for the run's machine — never split a run's workspace
	}

	// No holder yet: this placement establishes the run's workspace holder. Prefer
	// STABLE capacity (B2) — an interruptible machine reclaimed mid-run loses the
	// workspace and forces a from-scratch retry, so a run's home should be
	// non-reclaimable. Interruptible machines are used only as OVERFLOW, when no
	// stable machine has room. Within a class it's best-fit (smallest sufficient
	// free CPU, so big future requests keep a machine to land on).
	if best := bestFit(candidates, a, false); best != nil {
		return best
	}
	return bestFit(candidates, a, true)
}

// bestFit returns the smallest-sufficient-CPU candidate with room for a. When
// allowInterruptible is false, reclaimable machines are skipped.
func bestFit(candidates []*candidate, a db.ClaimPendingAssignmentsRow, allowInterruptible bool) *candidate {
	var best *candidate
	for _, c := range candidates {
		if c.freeCPU < a.CpuMillis || c.freeMem < a.MemoryMb {
			continue
		}
		if c.interruptible && !allowInterruptible {
			continue
		}
		if best == nil || c.freeCPU < best.freeCPU {
			best = c
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
