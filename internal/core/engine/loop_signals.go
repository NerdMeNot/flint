package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// maxParkedPerTick bounds how many parked steps (gates/waits) one tick resolves
// so signal processing can't starve the loop's other phases.
const maxParkedPerTick = 50

// processSignals approves waiting gates that have an approval signal. Each gate is
// claimed (row-locked via LockNextApprovedGate), transitioned, and advanced in one
// transaction — atomic, exactly-once across workers (SKIP LOCKED), and
// first-writer-wins against a racing rejection (the status='waiting' predicate).
func (l *Loop) processSignals(ctx context.Context) {
	for i := 0; i < maxParkedPerTick; i++ {
		if done := l.approveNextGate(ctx); done {
			return
		}
	}
}

func (l *Loop) approveNextGate(ctx context.Context) (done bool) {
	tx, err := l.pool.Begin(ctx)
	if err != nil {
		return true
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(l.pool).WithTx(tx)

	g, err := qtx.LockNextApprovedGate(ctx)
	if err != nil {
		if err != pgx.ErrNoRows {
			log.Warn().Err(err).Msg("engine: lock next approved gate")
		}
		return true
	}

	if err := qtx.ConsumeSignal(ctx, g.SignalID); err != nil {
		log.Error().Err(err).Str("step", g.StepName).Msg("engine: failed to consume approval signal")
		return true
	}
	approveResult := StepResult{StepName: g.StepName, Success: true}
	if err := transitionStep(ctx, qtx, stepTransition{
		stepID: g.StepID, workflowID: g.WorkflowID, stepName: g.StepName, attempt: int(g.Attempt),
		from: stepWaiting, to: stepSucceeded, result: &approveResult, eventType: "gate_approved",
	}); err != nil {
		log.Error().Err(err).Str("step", g.StepName).Msg("engine: failed to approve gate step")
		return true
	}
	if err := qtx.CancelTimer(ctx, db.CancelTimerParams{
		WorkflowID: g.WorkflowID, StepName: g.StepName, TimerType: timerGateTimeout,
	}); err != nil {
		log.Warn().Err(err).Str("step", g.StepName).Msg("engine: failed to cancel gate timer")
	}
	if err := advanceWorkflow(ctx, qtx, g.WorkflowID, 0); err != nil {
		log.Error().Err(err).Str("workflow", g.WorkflowID).Msg("engine: failed to advance after gate approval")
		return true
	}
	if err := tx.Commit(ctx); err != nil {
		log.Error().Err(err).Str("step", g.StepName).Msg("engine: failed to commit gate approval")
		return true
	}
	log.Info().Str("step", g.StepName).Msg("engine: gate approved")
	_ = db.New(l.pool).NotifyEngine(ctx, g.WorkflowID)
	l.engine.notifyState(ctx, g.WorkflowID)
	return false
}

// processRejections fails waiting gates that have a rejection signal so that
// `when: onFailure` steps can run. Same atomic, first-writer-wins claim as
// approvals — a gate already approved this tick is no longer 'waiting' and is
// skipped.
func (l *Loop) processRejections(ctx context.Context) {
	for i := 0; i < maxParkedPerTick; i++ {
		if done := l.rejectNextGate(ctx); done {
			return
		}
	}
}

func (l *Loop) rejectNextGate(ctx context.Context) (done bool) {
	tx, err := l.pool.Begin(ctx)
	if err != nil {
		return true
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(l.pool).WithTx(tx)

	r, err := qtx.LockNextRejectedGate(ctx)
	if err != nil {
		if err != pgx.ErrNoRows {
			log.Warn().Err(err).Msg("engine: lock next rejected gate")
		}
		return true
	}

	reason := "rejected"
	var payload struct {
		Reason string `json:"reason"`
	}
	if json.Unmarshal(r.Payload, &payload) == nil && payload.Reason != "" {
		reason = "rejected: " + payload.Reason
	}

	if err := qtx.ConsumeSignal(ctx, r.SignalID); err != nil {
		log.Error().Err(err).Str("step", r.StepName).Msg("engine: failed to consume rejection signal")
		return true
	}
	rejectResult := StepResult{StepName: r.StepName, Success: false, Error: reason}
	if err := transitionStep(ctx, qtx, stepTransition{
		stepID: r.StepID, workflowID: r.WorkflowID, stepName: r.StepName, attempt: int(r.Attempt),
		from: stepWaiting, to: stepFailed, result: &rejectResult, eventType: "gate_rejected", reason: reason,
	}); err != nil {
		log.Error().Err(err).Str("step", r.StepName).Msg("engine: failed to reject gate step")
		return true
	}
	if err := qtx.CancelTimer(ctx, db.CancelTimerParams{
		WorkflowID: r.WorkflowID, StepName: r.StepName, TimerType: timerGateTimeout,
	}); err != nil {
		log.Warn().Err(err).Str("step", r.StepName).Msg("engine: failed to cancel gate timer")
	}
	if err := advanceWorkflow(ctx, qtx, r.WorkflowID, 0); err != nil {
		log.Error().Err(err).Str("workflow", r.WorkflowID).Msg("engine: failed to advance after gate rejection")
		return true
	}
	if err := tx.Commit(ctx); err != nil {
		log.Error().Err(err).Str("step", r.StepName).Msg("engine: failed to commit gate rejection")
		return true
	}
	log.Info().Str("step", r.StepName).Str("reason", reason).Msg("engine: gate rejected")
	_ = db.New(l.pool).NotifyEngine(ctx, r.WorkflowID)
	l.engine.notifyState(ctx, r.WorkflowID)
	return false
}

// processSignalWaits resolves 'wait' steps whose configured external signal has
// arrived. The signal payload is captured into the step's outputs so downstream
// steps can reference steps.<name>.<key>. Same atomic lock-and-advance pattern as
// gates.
func (l *Loop) processSignalWaits(ctx context.Context) {
	for i := 0; i < maxParkedPerTick; i++ {
		if done := l.resolveNextWaitStep(ctx); done {
			return
		}
	}
}

func (l *Loop) resolveNextWaitStep(ctx context.Context) (done bool) {
	tx, err := l.pool.Begin(ctx)
	if err != nil {
		return true
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(l.pool).WithTx(tx)

	w, err := qtx.LockNextSignaledWaitStep(ctx)
	if err != nil {
		if err != pgx.ErrNoRows {
			log.Warn().Err(err).Msg("engine: lock next signaled wait step")
		}
		return true
	}

	// Capture the signal payload (a flat JSON object) into the step outputs.
	outputs := map[string]string{}
	var raw map[string]any
	if json.Unmarshal(w.Payload, &raw) == nil {
		for k, v := range raw {
			outputs[k] = fmt.Sprint(v)
		}
	}
	result := StepResult{StepName: w.StepName, Success: true, Outputs: outputs}

	if err := qtx.ConsumeSignal(ctx, w.SignalID); err != nil {
		log.Error().Err(err).Str("step", w.StepName).Msg("engine: failed to consume wait signal")
		return true
	}
	if err := transitionStep(ctx, qtx, stepTransition{
		stepID: w.StepID, workflowID: w.WorkflowID, stepName: w.StepName, attempt: int(w.Attempt),
		from: stepWaiting, to: stepSucceeded, result: &result, eventType: "wait_signaled",
	}); err != nil {
		log.Error().Err(err).Str("step", w.StepName).Msg("engine: failed to resolve wait step")
		return true
	}
	if err := qtx.UpdateStepOutputs(ctx, db.UpdateStepOutputsParams{
		ID:      w.WorkflowID,
		Column2: mustJSON(map[string]StepResult{w.StepName: result}),
	}); err != nil {
		log.Error().Err(err).Str("step", w.StepName).Msg("engine: failed to record wait outputs")
		return true
	}
	if err := qtx.CancelTimer(ctx, db.CancelTimerParams{
		WorkflowID: w.WorkflowID, StepName: w.StepName, TimerType: timerWaitTimeout,
	}); err != nil {
		log.Warn().Err(err).Str("step", w.StepName).Msg("engine: failed to cancel wait timer")
	}
	if err := advanceWorkflow(ctx, qtx, w.WorkflowID, 0); err != nil {
		log.Error().Err(err).Str("workflow", w.WorkflowID).Msg("engine: failed to advance after wait signal")
		return true
	}
	if err := tx.Commit(ctx); err != nil {
		log.Error().Err(err).Str("step", w.StepName).Msg("engine: failed to commit wait resolution")
		return true
	}
	log.Info().Str("step", w.StepName).Msg("engine: wait step signaled")
	_ = db.New(l.pool).NotifyEngine(ctx, w.WorkflowID)
	l.engine.notifyState(ctx, w.WorkflowID)
	return false
}

// processStepResultSignals advances every running workflow that has an
// unconsumed informer step-result signal. advanceWorkflow consumes the signal
// (marking the crashed step terminal) and queues whatever became eligible —
// all in one transaction per workflow.
func (l *Loop) processStepResultSignals(ctx context.Context) {
	q := db.New(l.pool)
	wfIDs, err := q.WorkflowsWithPendingStepSignals(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("engine: list workflows with pending step signals failed")
		return
	}
	for _, wfID := range wfIDs {
		tx, txErr := l.pool.Begin(ctx)
		if txErr != nil {
			log.Warn().Err(txErr).Msg("engine: begin tx for step-result signal advance")
			continue
		}
		qtx := db.New(l.pool).WithTx(tx)
		if err := advanceWorkflow(ctx, qtx, wfID, 0); err != nil {
			log.Warn().Err(err).Str("workflow", wfID).Msg("engine: step-result signal advance failed")
			_ = tx.Rollback(ctx)
			continue
		}
		if err := tx.Commit(ctx); err != nil {
			_ = tx.Rollback(ctx)
			continue
		}
		_ = q.NotifyEngine(ctx, wfID)
		l.engine.notifyState(ctx, wfID)
	}
}
