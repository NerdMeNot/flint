package fleet

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// restartReconcileGrace is how long a 'running' assignment may be absent from
// the agent's heartbeat active list before it is failed — long enough for a
// just-claimed assignment to appear in the next beat, short enough that an
// agent restart doesn't wedge a step until its timeout.
const restartReconcileGrace = 30 * time.Second

// HeartbeatInput is the agent's periodic self-report.
type HeartbeatInput struct {
	ActiveAssignmentIDs []string
	ResidentRunIDs      []string
	DrainRequested      bool
	DrainReason         string
	AgentVersion        string
}

// Cancellation identifies one running assignment the agent must kill.
type Cancellation struct {
	AssignmentID string
	RunID        string
	StepName     string
}

// HeartbeatResult is the server's command channel back to the agent.
type HeartbeatResult struct {
	Action        string // continue | drain | shutdown
	Interval      time.Duration
	Cancellations []Cancellation
	GCRunIDs      []string
}

// Heartbeat renews the machine's lease and returns pending commands. machine
// is the authenticated row from the token interceptor.
func (f *Fleet) Heartbeat(ctx context.Context, machine db.Machine, in HeartbeatInput) (HeartbeatResult, error) {
	q := db.New(f.pool)
	expiresAt := time.Now().Add(3 * DefaultHeartbeatInterval)
	if err := q.TouchMachineHeartbeat(ctx, db.TouchMachineHeartbeatParams{
		ID: machine.ID, HeartbeatExpiresAt: &expiresAt, AgentVersion: optStr(in.AgentVersion),
	}); err != nil {
		return HeartbeatResult{}, err
	}

	status := machine.Status
	// A lost machine that heartbeats again has reappeared (network partition,
	// static box rebooting slower than the lease): bring it back.
	if status == machineLost {
		newStatus, err := f.transitionFromCurrent(ctx, machine.ID, machineTransition{
			machineID: machine.ID, to: machineIdle,
			eventType: "reappeared", actor: actorAgent,
			reason: "heartbeat resumed after lease expiry",
		}, machineLost)
		if err != nil {
			log.Error().Err(err).Str("machine", machine.ID).
				Msg("fleet: failed to revive a reappeared machine")
		} else {
			status = newStatus
		}
	}

	// Agent-initiated drain (spot interruption notice, operator signal).
	//
	// Resolved against the machine's CURRENT status, not the row this RPC
	// authenticated with. The scheduler moves machines idle → busy every tick,
	// so a heartbeat that started a moment earlier carries a status that is
	// routinely stale — and validating the transition against it silently
	// dropped the drain. The drain that matters most is the spot-interruption
	// notice, where losing it means the agent keeps taking work until the
	// instance is reclaimed mid-step.
	if in.DrainRequested {
		reason := orDefault(in.DrainReason, "agent_request")
		newStatus, err := f.transitionFromCurrent(ctx, machine.ID, machineTransition{
			machineID: machine.ID, to: machineDraining,
			eventType: "drain", actor: actorAgent, reason: reason,
			drainReason: &reason,
		}, machineIdle, machineBusy)
		switch {
		case err != nil:
			log.Error().Err(err).Str("machine", machine.ID).Str("reason", reason).
				Msg("fleet: failed to apply requested drain")
		default:
			if newStatus == machineDraining && status != machineDraining {
				log.Warn().Str("machine", machine.ID).Str("reason", reason).
					Msg("fleet: machine self-draining")
			}
			status = newStatus
		}
	}

	res := HeartbeatResult{Interval: DefaultHeartbeatInterval}
	switch status {
	case machineDraining:
		res.Action = "drain"
	case machineTerminating, machineTerminated, machineFailed:
		res.Action = "shutdown"
	default:
		res.Action = "continue"
	}

	// Cancellations for running assignments (redundant with the stream, so a
	// broken stream delays cancellation by at most one beat).
	cancels, err := q.ListCancelRequestedForMachine(ctx, &machine.ID)
	if err == nil {
		for _, c := range cancels {
			res.Cancellations = append(res.Cancellations, Cancellation{
				AssignmentID: c.ID, RunID: c.RunID, StepName: c.StepName,
			})
		}
	}

	// Workspace GC: which resident runs are terminal.
	if len(in.ResidentRunIDs) > 0 {
		if terminal, err := q.TerminalRunIDs(ctx, in.ResidentRunIDs); err == nil {
			res.GCRunIDs = terminal
		}
	}

	// Agent-restart recovery: running assignments the agent no longer claims
	// (it restarted and killed its containers) fail now instead of waiting for
	// the step timeout. Grace period covers claim/report races.
	f.reconcileActiveAssignments(ctx, machine.ID, in.ActiveAssignmentIDs)

	return res, nil
}

// reconcileActiveAssignments fails 'running' assignments missing from the
// agent's reported active list past the grace period.
func (f *Fleet) reconcileActiveAssignments(ctx context.Context, machineID string, activeIDs []string) {
	q := db.New(f.pool)
	if activeIDs == nil {
		activeIDs = []string{}
	}
	stale, err := q.StaleRunningAssignmentsForMachine(ctx, db.StaleRunningAssignmentsForMachineParams{
		MachineID: &machineID, GraceSecs: restartReconcileGrace.Seconds(), ActiveIds: activeIDs,
	})
	if err != nil || len(stale) == 0 {
		return
	}
	for _, a := range stale {
		errMsg := fmt.Sprintf("agent on machine %s no longer reports this step (agent restart)", machineID)
		failed, err := q.FailAssignment(ctx, db.FailAssignmentParams{ID: a.ID, Error: &errMsg})
		if err != nil {
			continue
		}
		f.failStepsViaSignals(ctx, []db.FailMachineAssignmentsRow{{
			ID: failed.ID, StepID: failed.StepID, WorkflowID: failed.WorkflowID,
			RunID: failed.RunID, StepName: failed.StepName, Attempt: failed.Attempt,
		}})
		log.Warn().Str("machine", machineID).Str("step", a.StepName).Str("run", a.RunID).
			Msg("fleet: assignment failed — agent restarted without it")
	}
}

// transitionFromCurrent applies t in its own transaction, resolving `from` by
// re-reading the machine under lock. It returns the machine's status afterwards.
//
// applicableFrom lists the statuses the transition makes sense from. If the
// machine is in none of them the call is a no-op and the current status is
// returned — the caller asked for something that no longer applies, which is not
// an error. Anything else (an illegal edge, a DB failure) IS returned, because
// the alternative is a request the agent believes was honoured and wasn't.
func (f *Fleet) transitionFromCurrent(ctx context.Context, machineID string, t machineTransition, applicableFrom ...string) (string, error) {
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(f.pool).WithTx(tx)

	m, err := qtx.LockMachine(ctx, machineID)
	if err != nil {
		return "", err
	}
	if !slices.Contains(applicableFrom, m.Status) {
		return m.Status, nil
	}
	t.from = m.Status
	if err := transitionMachine(ctx, qtx, t); err != nil {
		return "", err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", err
	}
	return t.to, nil
}
