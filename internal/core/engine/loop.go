package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Loop is the worker main loop. It polls Postgres for queued steps,
// fires timers, dispatches steps via a StepExecutor, and sweeps for stale state.
type Loop struct {
	pool      db.Pool
	engine    *PgEngine
	executors ExecutorRegistry
	config    LoopConfig
	wake      chan struct{}
}

// NewLoop creates a worker loop. executors maps step exec types to the executor
// that runs them; an empty registry runs the loop in DB-only mode (steps are
// claimed but never dispatched).
func NewLoop(engine *PgEngine, executors ExecutorRegistry, cfg LoopConfig) *Loop {
	return &Loop{
		pool:      engine.pool,
		engine:    engine,
		executors: executors,
		config:    cfg,
		wake:      make(chan struct{}, 1),
	}
}

// Run starts the main loop. Blocks until context cancellation.
func (l *Loop) Run(ctx context.Context) error {
	log.Info().
		Dur("pollInterval", l.config.pollInterval()).
		Dur("sweepInterval", l.config.sweepInterval()).
		Msg("engine: worker loop started")

	// Start LISTEN/NOTIFY listener for instant wakeup.
	go l.listenNotify(ctx)

	ticker := time.NewTicker(l.config.pollInterval())
	sweepTicker := time.NewTicker(l.config.sweepInterval())
	defer ticker.Stop()
	defer sweepTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("engine: worker loop stopped")
			return nil
		case <-l.wake:
			l.tick(ctx)
		case <-ticker.C:
			l.tick(ctx)
		case <-sweepTicker.C:
			l.sweep(ctx)
		}
	}
}

// tick is one iteration of the main loop.
func (l *Loop) tick(ctx context.Context) {
	// Phase 1: Fire due timers.
	if err := fireTimers(ctx, l.pool); err != nil {
		log.Error().Err(err).Msg("engine: fire timers error")
	}

	// Phase 2: Process gate approval/rejection signals and external-signal waits.
	l.processSignals(ctx)
	l.processRejections(ctx)
	l.processSignalWaits(ctx)

	// Phase 3: Process outbox events (webhook delivery).
	processOutbox(ctx, l.pool)

	// Phase 4: Claim and dispatch queued steps.
	l.claimAndDispatch(ctx)

	// Phase 5: Tear down resources for runs that just reached a terminal state.
	l.cleanupFinishedRuns(ctx)
}

// cleanupFinishedRuns tears down executor resources (workspace pod, leftover
// Jobs) for terminal runs not yet cleaned. Exactly-once via
// pipeline_runs.cleaned_at; runs every tick so cleanup is prompt for ALL
// terminal transitions — cancel, fail, succeed — not just the sweep window.
// Marks runs cleaned even when no executor needs cleanup (e.g. http-only or
// DB-only mode) so the pending-cleanup index stays bounded.
func (l *Loop) cleanupFinishedRuns(ctx context.Context) {
	q := db.New(l.pool)
	runIDs, err := q.RunsNeedingCleanup(ctx, 100)
	if err != nil {
		log.Warn().Err(err).Msg("engine: list runs needing cleanup failed")
		return
	}
	cleaners := l.executors.cleaners()
	for _, runID := range runIDs {
		for _, c := range cleaners {
			_ = c.CleanupRun(ctx, runID)
		}
		if err := q.MarkRunCleaned(ctx, runID); err != nil {
			log.Warn().Err(err).Str("run", runID).Msg("engine: mark run cleaned failed")
		}
	}
}

// claimAndDispatch claims queued steps and dispatches them.
func (l *Loop) claimAndDispatch(ctx context.Context) {
	l.claimAndDispatchSimple(ctx)
}

func (l *Loop) claimAndDispatchSimple(ctx context.Context) {
	q := db.New(l.pool)
	claimedSteps, err := q.ClaimQueuedSteps(ctx, int32(l.config.claimBatchSize()))
	if err != nil {
		log.Error().Err(err).Msg("engine: claim steps error")
		return
	}
	if len(claimedSteps) == 0 {
		return
	}

	// Batch-fetch the workflow inputs for the whole claimed batch in one round-trip
	// (steps from the same run share an input) instead of one query per step.
	inputs := l.loadWorkflowInputs(ctx, q, claimedSteps)

	for _, c := range claimedSteps {
		token := EncodeTaskToken(TaskToken{
			WorkflowID: c.WorkflowID,
			StepName:   c.Name,
			Attempt:    int(c.Attempt),
		}, l.config.SigningKey)

		// Update task token on the step.
		if err := q.SetStepTaskToken(ctx, db.SetStepTaskTokenParams{
			ID:        c.ID,
			TaskToken: &token,
		}); err != nil {
			log.Error().Err(err).Str("step", c.Name).Msg("engine: failed to set task token")
		}

		input, ok := inputs[c.WorkflowID]
		if !ok {
			log.Error().Str("step", c.Name).Msg("engine: missing workflow input for claimed step")
			continue
		}

		// Parked steps (gate approval, external-signal wait) get a durable timeout
		// timer when claimed; they are never dispatched to an executor.
		switch c.ExecType {
		case execGate:
			var gateStep struct {
				Timeout string `json:"timeout"`
			}
			_ = json.Unmarshal(c.StepDef, &gateStep)
			l.createWaitTimer(ctx, c.WorkflowID, c.Name, timerGateTimeout, gateStep.Timeout, 4*time.Hour)
		case execWait:
			var waitStep struct {
				Wait struct {
					Timeout string `json:"timeout"`
				} `json:"wait"`
			}
			_ = json.Unmarshal(c.StepDef, &waitStep)
			l.createWaitTimer(ctx, c.WorkflowID, c.Name, timerWaitTimeout, waitStep.Wait.Timeout, 24*time.Hour)
		}

		// Dispatch run/use/steps steps. Gates/waits are parked (status 'waiting').
		if c.Status == stepRunning && len(l.executors) > 0 {
			l.dispatchClaimedStep(ctx, q, c, token, input)
		}
	}
}

// loadWorkflowInputs fetches and decodes the workflow input for every distinct
// workflow in the claimed batch in a single query.
func (l *Loop) loadWorkflowInputs(ctx context.Context, q *db.Queries, steps []db.ClaimQueuedStepsRow) map[string]StartWorkflowInput {
	seen := make(map[string]bool, len(steps))
	ids := make([]string, 0, len(steps))
	for _, c := range steps {
		if !seen[c.WorkflowID] {
			seen[c.WorkflowID] = true
			ids = append(ids, c.WorkflowID)
		}
	}
	out := make(map[string]StartWorkflowInput, len(ids))
	rows, err := q.GetWorkflowInputs(ctx, ids)
	if err != nil {
		log.Error().Err(err).Msg("engine: batch fetch workflow inputs failed")
		return out
	}
	for _, r := range rows {
		var in StartWorkflowInput
		if json.Unmarshal(r.Input, &in) == nil {
			out[r.ID] = in
		}
	}
	return out
}

// dispatchClaimedStep resolves env/secrets, enforces the org concurrency limit,
// and hands a claimed running step to its executor. On a transient dispatch error
// the step is routed through the retry policy (not failed outright); on success
// it records dispatched_at so the undispatched-step sweep won't reclaim it.
func (l *Loop) dispatchClaimedStep(ctx context.Context, q *db.Queries, c db.ClaimQueuedStepsRow, token string, input StartWorkflowInput) {
	// Enforce per-org concurrency limit: if the org is at capacity, push this one
	// back to 'queued' to be picked up when a slot opens.
	if input.OrgID != "" && l.checkConcurrencyLimit(ctx, q, input.OrgID, c.ID) {
		log.Debug().Str("step", c.Name).Str("org", input.OrgID).
			Msg("engine: org concurrency limit reached, re-queuing step")
		observe.StepsThrottled.Add(ctx, 1, metric.WithAttributes(attribute.String("org_id", input.OrgID)))
		return
	}

	// Resolve env vars: org env_variables → step.env → input.Env
	merged := l.resolveStepEnv(ctx, q, input, c.StepDef)

	// Extract secret mapping from step_def.
	var stepDef struct {
		Secrets map[string]string `json:"secrets"`
	}
	_ = json.Unmarshal(c.StepDef, &stepDef)

	step := claimedStep{
		id: c.ID, workflowID: c.WorkflowID, name: c.Name,
		execType: c.ExecType, taskToken: token, stepDef: c.StepDef,
		wsToken: DeriveWorkspaceToken(input.RunID, l.config.SigningKey),
		runID:   input.RunID, orgID: input.OrgID, projectID: input.ProjectID,
		repo: input.Repo, ref: input.Ref, commitSHA: input.CommitSHA,
		environment:            input.Environment,
		pipelineImage:          input.PipelineImage,
		pipelineServiceAccount: input.PipelineServiceAccount,
		env:                    merged,
		secretMapping:          stepDef.Secrets,
	}
	handle, err := dispatchStep(ctx, l.executors, step)
	if errors.Is(err, errNoExecutor) {
		// No executor for this step type (e.g. DB-only mode) — leave the step
		// running; it'll be picked up if an executor appears.
		return
	}
	if err != nil {
		log.Error().Err(err).Str("step", c.Name).Msg("engine: dispatch failed")
		observe.DispatchErrors.Add(ctx, 1, metric.WithAttributes(attribute.String("step", c.Name)))
		// Route through the retry policy: a transient dispatch error (e.g. a k8s API
		// hiccup) backs off and retries like any failure, rather than terminally
		// failing the step. If attempts are exhausted it fails and advances so the
		// workflow doesn't wedge.
		l.failStepAndAdvance(ctx, c.WorkflowID, c.Name, int(c.Attempt), err)
		return
	}

	observe.StepsDispatched.Add(ctx, 1)
	// Record dispatch: stamps dispatched_at (so the undispatched sweep ignores this
	// step) and the executor handle (e.g. k8s Job name) for correlation. handle is
	// empty for in-process executors (http/sim) — store NULL.
	var handlePtr *string
	if handle != "" {
		handlePtr = &handle
	}
	if err := q.MarkStepDispatched(ctx, db.MarkStepDispatchedParams{ID: c.ID, K8sJobName: handlePtr}); err != nil {
		log.Warn().Err(err).Str("step", c.Name).Msg("engine: failed to record step dispatch")
	}
}

// failStepAndAdvance handles a dispatch failure: it re-locks the step to read its
// retry policy, marks the attempt failed, schedules a backoff retry if attempts
// remain, and advances the workflow — all in one transaction. Without the advance
// the workflow would wedge (the failed step is terminal, downstream steps stay
// pending, and the sweep only rescues 'running' steps past deadline).
func (l *Loop) failStepAndAdvance(ctx context.Context, workflowID, stepName string, attempt int, cause error) {
	tx, err := l.pool.Begin(ctx)
	if err != nil {
		log.Error().Err(err).Str("step", stepName).Msg("engine: failStepAndAdvance: begin tx")
		return
	}
	defer tx.Rollback(ctx)
	qtx := db.New(l.pool).WithTx(tx)

	stepRow, err := qtx.LockStep(ctx, db.LockStepParams{
		WorkflowID: workflowID, Name: stepName, Attempt: int32(attempt),
	})
	if err != nil {
		log.Error().Err(err).Str("step", stepName).Msg("engine: failStepAndAdvance: lock step")
		return
	}
	if isTerminal(stepRow.Status) {
		return // already resolved by another path
	}

	if err := qtx.UpdateStepResult(ctx, db.UpdateStepResultParams{
		ID:     stepRow.ID,
		Status: stepFailed,
		Result: mustJSON(StepResult{StepName: stepName, Success: false, Error: cause.Error()}),
	}); err != nil {
		log.Error().Err(err).Str("step", stepName).Msg("engine: failed to mark step failed after dispatch error")
		return
	}
	if _, err := maybeScheduleRetry(ctx, qtx, stepRow.ID, workflowID, stepName,
		attempt, int(stepRow.MaxAttempts), stepRow.RetryBackoff, int(stepRow.RetryIntervalSeconds)); err != nil {
		log.Error().Err(err).Str("step", stepName).Msg("engine: failed to schedule retry after dispatch error")
		return
	}
	if err := advanceWorkflow(ctx, qtx, workflowID, 0); err != nil {
		log.Error().Err(err).Str("workflow", workflowID).Msg("engine: advance after dispatch failure")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		log.Error().Err(err).Str("step", stepName).Msg("engine: commit after dispatch failure")
		return
	}
	_ = db.New(l.pool).NotifyEngine(ctx, workflowID)
}

// createWaitTimer creates a durable timeout timer for a parked step (gate approval
// or external-signal wait). rawTimeout is the step's configured timeout (e.g.
// "1h"); if empty or unparseable it falls back to def. CreateTimer is idempotent
// (ON CONFLICT DO NOTHING) so re-claiming a step doesn't reset the deadline.
func (l *Loop) createWaitTimer(ctx context.Context, workflowID, stepName, timerType, rawTimeout string, def time.Duration) {
	timeout := def
	if rawTimeout != "" {
		if d, err := time.ParseDuration(rawTimeout); err == nil && d > 0 {
			timeout = d
		}
	}
	if err := db.New(l.pool).CreateTimer(ctx, db.CreateTimerParams{
		WorkflowID: workflowID,
		StepName:   stepName,
		TimerType:  timerType,
		Secs:       timeout.Seconds(),
	}); err != nil {
		log.Error().Err(err).Str("step", stepName).Str("type", timerType).Dur("timeout", timeout).
			Msg("engine: failed to create wait timeout timer")
	}
}

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
	defer tx.Rollback(ctx)
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
	if err := qtx.UpdateStepResult(ctx, db.UpdateStepResultParams{
		ID:     g.StepID,
		Status: stepSucceeded,
		Result: mustJSON(StepResult{StepName: g.StepName, Success: true}),
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
	defer tx.Rollback(ctx)
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
	if err := qtx.UpdateStepResult(ctx, db.UpdateStepResultParams{
		ID:     r.StepID,
		Status: stepFailed,
		Result: mustJSON(StepResult{StepName: r.StepName, Success: false, Error: reason}),
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
	defer tx.Rollback(ctx)
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
	if err := qtx.UpdateStepResult(ctx, db.UpdateStepResultParams{
		ID:     w.StepID,
		Status: stepSucceeded,
		Result: mustJSON(result),
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

// sweep detects and recovers from stale state.
func (l *Loop) sweep(ctx context.Context) {
	log.Debug().Msg("engine: sweep started")

	q := db.New(l.pool)

	// 1. Recover steps stuck mid-lifecycle, then advance any workflow that just had
	//    a step fail so the failure propagates (run onFailure, skip downstream,
	//    finish) instead of wedging.
	failed := false

	// 1a. Running steps past their execution deadline.
	if count, err := q.SweepStaleRunningSteps(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: sweep stale running steps failed")
	} else if count > 0 {
		log.Warn().Int64("count", count).Msg("engine: sweep recovered stale steps")
		failed = true
	}

	// 1b. Backstop: gate/wait steps still 'waiting' although their timeout timer
	//     already fired (should never happen with atomic timer handling).
	if count, err := q.FailOrphanedWaitingSteps(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: sweep orphaned waiting steps failed")
	} else if count > 0 {
		log.Warn().Int64("count", count).Msg("engine: sweep failed orphaned waiting steps")
		failed = true
	}

	if failed {
		l.advanceRecentlyFailed(ctx, q)
	}

	// 1c. Steps claimed (running) but never dispatched — the worker died between
	//     claiming and creating the Job. Re-queue them (they never executed). Only
	//     meaningful when executors are registered (DB-only mode leaves steps
	//     running on purpose).
	if len(l.executors) > 0 {
		if count, err := q.RequeueUndispatchedSteps(ctx, l.config.dispatchGrace().Seconds()); err != nil {
			log.Warn().Err(err).Msg("engine: sweep requeue undispatched steps failed")
		} else if count > 0 {
			log.Warn().Int64("count", count).Msg("engine: sweep re-queued undispatched steps")
		}
	}

	// 2. Stale workflows where all steps are terminal but workflow still "running".
	if err := q.SweepStaleWorkflows(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: sweep stale workflows failed")
	}

	// 3. Recover outbox events stranded in 'processing' by a worker crash.
	if count, err := q.RecoverStaleOutboxEvents(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: recover stale outbox events failed")
	} else if count > 0 {
		log.Warn().Int64("count", count).Msg("engine: recovered stranded outbox events")
	}

	// 4. Clean up fired timers older than 1 hour, resolved outbox events (>7d),
	// and consumed signals (>7d) so these tables don't grow unbounded.
	if err := q.CleanupFiredTimers(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: cleanup fired timers failed")
	}
	if err := q.CleanResolvedOutbox(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: cleanup resolved outbox failed")
	}
	if err := q.DeleteConsumedSignals(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: cleanup consumed signals failed")
	}

	// 5. Tear down resources for terminal runs not yet cleaned. Same exactly-once
	// path as the per-tick cleanup; kept here as a backstop in case a run reached
	// a terminal state without a following tick.
	l.cleanupFinishedRuns(ctx)

	log.Debug().Msg("engine: sweep completed")
}

// advanceRecentlyFailed advances every workflow that had a step fail in the last
// few seconds, so a sweep-induced failure propagates to downstream steps. Each
// advance runs in its own transaction.
func (l *Loop) advanceRecentlyFailed(ctx context.Context, q *db.Queries) {
	wfIDs, err := q.RecentlyFailedWorkflowIDs(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("engine: sweep failed to list recently failed workflows")
		return
	}
	for _, wfID := range wfIDs {
		tx, txErr := l.pool.Begin(ctx)
		if txErr != nil {
			log.Warn().Err(txErr).Msg("engine: sweep failed to begin tx for advancement")
			continue
		}
		qtx := db.New(l.pool).WithTx(tx)
		if err := advanceWorkflow(ctx, qtx, wfID, 0); err != nil {
			log.Warn().Err(err).Str("workflow", wfID).Msg("engine: sweep advancement failed")
		}
		if err := tx.Commit(ctx); err != nil {
			tx.Rollback(ctx)
		}
	}
}

// resolveStepEnv merges env vars from three sources with proper precedence:
// org env_variables (base) < step YAML env: (override) < workflow input.Env (top).
func (l *Loop) resolveStepEnv(ctx context.Context, q *db.Queries, input StartWorkflowInput, stepDefJSON []byte) map[string]string {
	merged := make(map[string]string)

	// 1. Org-level env_variables (global + environment-scoped).
	envVars, err := q.ResolveEnvVars(ctx, db.ResolveEnvVarsParams{
		OrgID:   input.OrgID,
		EnvSlug: input.Environment,
	})
	if err == nil {
		for _, ev := range envVars {
			merged[ev.Name] = ev.Value
		}
	}

	// 2. Step-level env: from pipeline YAML (overrides org vars).
	var stepDef struct {
		Env map[string]string `json:"env"`
	}
	if json.Unmarshal(stepDefJSON, &stepDef) == nil {
		for k, v := range stepDef.Env {
			merged[k] = v
		}
	}

	// 3. Workflow-level env from trigger (highest precedence).
	for k, v := range input.Env {
		merged[k] = v
	}

	return merged
}

// checkConcurrencyLimit reports whether the org is over its concurrent-step limit
// and, if so, reverts this step to 'queued' (throttled → true). The count and the
// throttle decision run inside one transaction holding a per-org advisory lock, so
// concurrent workers can't both observe headroom and both dispatch past the limit
// (the previous count-then-compare was racy). The step was already claimed to
// 'running', so it is included in the count: running > limit means this step tips
// the org over and must wait.
func (l *Loop) checkConcurrencyLimit(ctx context.Context, q *db.Queries, orgID, stepID string) bool {
	limit, err := q.GetOrgConcurrencyLimit(ctx, orgID)
	if err != nil {
		return false // can't check — allow dispatch
	}

	tx, err := l.pool.Begin(ctx)
	if err != nil {
		return false
	}
	defer tx.Rollback(ctx)
	qtx := db.New(l.pool).WithTx(tx)

	if err := qtx.LockOrgConcurrency(ctx, orgID); err != nil {
		return false // couldn't take the lock — allow dispatch rather than stall
	}
	running, err := qtx.CountRunningStepsByOrg(ctx, orgID)
	if err != nil {
		return false
	}
	if running <= int64(limit) {
		_ = tx.Commit(ctx) // release the advisory lock
		return false
	}

	// Over limit — re-queue this step so it's picked up when a slot opens.
	if err := qtx.SetStepQueued(ctx, stepID); err != nil {
		log.Error().Err(err).Str("stepID", stepID).Msg("engine: failed to re-queue throttled step")
		return false
	}
	if err := tx.Commit(ctx); err != nil {
		log.Error().Err(err).Str("stepID", stepID).Msg("engine: failed to commit throttle re-queue")
		return false
	}
	return true
}

// listenNotify listens for Postgres NOTIFY events for instant wakeup.
// Requires the underlying pool to be *pgxpool.Pool (for Acquire). If the pool
// implementation doesn't support Acquire, falls back to polling-only mode.
func (l *Loop) listenNotify(ctx context.Context) {
	pgPool, ok := l.pool.(*pgxpool.Pool)
	if !ok {
		log.Warn().Msg("engine: pool does not support Acquire, LISTEN/NOTIFY disabled (polling-only mode)")
		return
	}

	for {
		conn, err := pgPool.Acquire(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			time.Sleep(time.Second)
			continue
		}

		_, err = conn.Exec(ctx, "LISTEN flint_engine")
		if err != nil {
			conn.Release()
			time.Sleep(time.Second)
			continue
		}

		for {
			_, err := conn.Conn().WaitForNotification(ctx)
			if err != nil {
				conn.Release()
				if ctx.Err() != nil {
					return
				}
				break // reconnect
			}
			select {
			case l.wake <- struct{}{}:
			default:
			}
		}
	}
}
