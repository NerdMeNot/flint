package fleet

import (
	"context"
	"fmt"
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
		if err := f.transitionInTx(ctx, machineTransition{
			machineID: machine.ID, from: machineLost, to: machineIdle,
			eventType: "reappeared", actor: actorAgent,
			reason: "heartbeat resumed after lease expiry",
		}); err == nil {
			status = machineIdle
		}
	}

	// Agent-initiated drain (spot interruption notice, operator signal).
	if in.DrainRequested && (status == machineIdle || status == machineBusy) {
		reason := orDefault(in.DrainReason, "agent_request")
		if err := f.transitionInTx(ctx, machineTransition{
			machineID: machine.ID, from: status, to: machineDraining,
			eventType: "drain", actor: actorAgent, reason: reason,
			drainReason: &reason,
		}); err == nil {
			status = machineDraining
			log.Warn().Str("machine", machine.ID).Str("reason", reason).
				Msg("fleet: machine self-draining")
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

// transitionInTx runs one transition in its own transaction, re-reading the
// current status under lock is skipped deliberately: heartbeat transitions are
// low-contention and validated against the status the caller just read.
func (f *Fleet) transitionInTx(ctx context.Context, t machineTransition) error {
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := transitionMachine(ctx, db.New(f.pool).WithTx(tx), t); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
