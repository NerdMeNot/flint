package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
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
	pool       db.Pool
	engine     *PgEngine
	executors  ExecutorRegistry
	config     LoopConfig
	wake       chan struct{}
	outboxWake chan struct{}
	// notifyHealthy tracks whether the LISTEN connection is live. When it is,
	// NOTIFY wakeups are the fast path and polling backs off to the idle
	// interval; when it drops, polling falls back to the tight interval.
	notifyHealthy atomic.Bool
}

// NewLoop creates a worker loop. executors maps step exec types to the executor
// that runs them; an empty registry runs the loop in DB-only mode (steps are
// claimed but never dispatched).
func NewLoop(engine *PgEngine, executors ExecutorRegistry, cfg LoopConfig) *Loop {
	return &Loop{
		pool:       engine.pool,
		engine:     engine,
		executors:  executors,
		config:     cfg,
		wake:       make(chan struct{}, 1),
		outboxWake: make(chan struct{}, 1),
	}
}

// Run starts the main loop. Blocks until context cancellation.
func (l *Loop) Run(ctx context.Context) error {
	log.Info().
		Dur("pollInterval", l.config.pollInterval()).
		Dur("idlePollInterval", l.config.idlePollInterval()).
		Dur("sweepInterval", l.config.sweepInterval()).
		Msg("engine: worker loop started")

	// Start LISTEN/NOTIFY listener for instant wakeup.
	go l.listenNotify(ctx)

	// Deliver outbox events (webhooks) on a dedicated goroutine so a slow endpoint
	// can never stall step claiming/dispatch on the tick loop.
	go l.runOutbox(ctx)

	// Adaptive polling: while LISTEN/NOTIFY is healthy, notifications carry the
	// work signal and the poll is only a safety net (timers still need it) —
	// back off to the idle interval. Without a healthy listener, poll tight.
	timer := time.NewTimer(l.config.pollInterval())
	sweepTicker := time.NewTicker(l.config.sweepInterval())
	defer timer.Stop()
	defer sweepTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("engine: worker loop stopped")
			return nil
		case <-l.wake:
			l.tick(ctx)
		case <-timer.C:
			l.tick(ctx)
		case <-sweepTicker.C:
			l.sweep(ctx)
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(l.currentPollInterval())
	}
}

// currentPollInterval returns the poll cadence for the current NOTIFY health.
// Timers cap the backoff: a due timer must not wait for a 30s poll, so when
// one fires sooner than the idle interval the wait shrinks to it.
func (l *Loop) currentPollInterval() time.Duration {
	if !l.notifyHealthy.Load() {
		return l.config.pollInterval()
	}
	idle := l.config.idlePollInterval()
	if due, err := db.New(l.pool).NextTimerDue(context.Background()); err == nil && !due.IsZero() {
		if wait := time.Until(due); wait < idle {
			if wait < l.config.pollInterval() {
				return l.config.pollInterval()
			}
			return wait
		}
	}
	return idle
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

	// Phase 2.5: Advance workflows with pending step-result signals.
	// This is the crash-recovery path: when an agent dies without reporting,
	// the fleet's machine-lost sweep delivers a step-result signal within
	// seconds — without this phase nothing would consume it until the step's
	// timeout sweep (hours later), because tick otherwise only advances via
	// completions.
	l.processStepResultSignals(ctx)

	// Phase 3: Claim and dispatch queued steps.
	l.claimAndDispatch(ctx)

	// Phase 4: Tear down resources for runs that just reached a terminal state.
	l.cleanupFinishedRuns(ctx)
}

// runOutbox delivers webhook outbox events on its own goroutine, decoupled from
// the tick loop. NOTIFY wakes it promptly (a finished/cancelled run enqueues
// webhook events and signals the engine channel); polling is only the fallback,
// so it follows the same adaptive cadence as the main loop.
func (l *Loop) runOutbox(ctx context.Context) {
	timer := time.NewTimer(l.config.pollInterval())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-l.outboxWake:
			processOutbox(ctx, l.pool)
		case <-timer.C:
			processOutbox(ctx, l.pool)
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		if l.notifyHealthy.Load() {
			timer.Reset(l.config.idlePollInterval())
		} else {
			timer.Reset(l.config.pollInterval())
		}
	}
}

// cleanupFinishedRuns tears down executor resources (outstanding step
// assignments) for terminal runs not yet cleaned. Exactly-once via
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

	// Mint every task token up front and stamp them in ONE round-trip.
	tokens := make([]string, len(claimedSteps))
	ids := make([]string, len(claimedSteps))
	for i, c := range claimedSteps {
		ids[i] = c.ID
		tokens[i] = EncodeTaskToken(TaskToken{
			WorkflowID: c.WorkflowID,
			StepName:   c.Name,
			Attempt:    int(c.Attempt),
		}, l.config.SigningKey)
	}
	if err := q.SetStepTaskTokens(ctx, db.SetStepTaskTokensParams{Ids: ids, Tokens: tokens}); err != nil {
		log.Error().Err(err).Msg("engine: failed to set task tokens for claimed batch")
	}

	for i, c := range claimedSteps {
		token := tokens[i]

		// Record the claim transition for parked steps (gates/waits) here; for
		// running steps the "claimed" event is emitted AFTER the org
		// concurrency check in dispatchClaimedStep — a throttled step is
		// re-queued and re-claimed every tick, and emitting here would spam an
		// unbounded stream of claimed events into the run timeline.
		if c.Status == stepWaiting {
			emitStepEvent(ctx, q, stepTransition{
				workflowID: c.WorkflowID, stepName: c.Name, attempt: int(c.Attempt),
				from: stepQueued, to: c.Status, eventType: "parked",
			})
		}

		meta, ok := inputs[c.WorkflowID]
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
			l.dispatchClaimedStep(ctx, q, c, token, meta)
		}
	}
}

// workflowMeta bundles the per-workflow data a dispatch needs: the start
// input plus the accumulated step outputs (for needs.<job>.outputs.*).
type workflowMeta struct {
	input       StartWorkflowInput
	stepOutputs map[string]StepResult
}

// loadWorkflowInputs fetches and decodes the workflow input + step outputs for
// every distinct workflow in the claimed batch in a single query.
func (l *Loop) loadWorkflowInputs(ctx context.Context, q *db.Queries, steps []db.ClaimQueuedStepsRow) map[string]workflowMeta {
	seen := make(map[string]bool, len(steps))
	ids := make([]string, 0, len(steps))
	for _, c := range steps {
		if !seen[c.WorkflowID] {
			seen[c.WorkflowID] = true
			ids = append(ids, c.WorkflowID)
		}
	}
	out := make(map[string]workflowMeta, len(ids))
	rows, err := q.GetWorkflowInputs(ctx, ids)
	if err != nil {
		log.Error().Err(err).Msg("engine: batch fetch workflow inputs failed")
		return out
	}
	for _, r := range rows {
		var meta workflowMeta
		if json.Unmarshal(r.Input, &meta.input) != nil {
			continue
		}
		_ = json.Unmarshal(r.StepOutputs, &meta.stepOutputs)
		out[r.ID] = meta
	}
	return out
}

// dispatchClaimedStep resolves env/secrets, enforces the org concurrency limit,
// and hands a claimed running step to its executor. On a transient dispatch error
// the step is routed through the retry policy (not failed outright); on success
// it records dispatched_at so the undispatched-step sweep won't reclaim it.
func (l *Loop) dispatchClaimedStep(ctx context.Context, q *db.Queries, c db.ClaimQueuedStepsRow, token string, meta workflowMeta) {
	input := meta.input
	// Enforce per-org concurrency limit: if the org is at capacity, push this one
	// back to 'queued' to be picked up when a slot opens.
	if input.OrgID != "" && l.checkConcurrencyLimit(ctx, q, input.OrgID, c.ID) {
		log.Debug().Str("step", c.Name).Str("org", input.OrgID).
			Msg("engine: org concurrency limit reached, re-queuing step")
		observe.StepsThrottled.Add(ctx, 1, metric.WithAttributes(attribute.String("org_id", input.OrgID)))
		return
	}

	// Past the throttle — record the claim for the run timeline (exactly once
	// per real dispatch attempt, not per throttled re-queue).
	emitStepEvent(ctx, q, stepTransition{
		workflowID: c.WorkflowID, stepName: c.Name, attempt: int(c.Attempt),
		from: stepQueued, to: stepRunning, eventType: "claimed",
	})

	// Resolve env vars: org env_variables → step.env → input.Env
	merged := l.resolveStepEnv(ctx, q, input, c.StepDef)

	// Extract secret mapping + dependency edges from step_def.
	var stepDef struct {
		Secrets   map[string]string `json:"secrets"`
		DependsOn []string          `json:"dependsOn"`
	}
	_ = json.Unmarshal(c.StepDef, &stepDef)

	// needs.<job>.outputs.* for the in-container driver: the outputs of this step's
	// direct dependencies, keyed by base job name (matrix suffix stripped).
	needsOutputs := map[string]map[string]string{}
	for _, dep := range stepDef.DependsOn {
		if r, ok := meta.stepOutputs[dep]; ok && len(r.Outputs) > 0 {
			needsOutputs[baseStepName(dep)] = r.Outputs
		}
	}

	step := claimedStep{
		id: c.ID, workflowID: c.WorkflowID, name: c.Name,
		execType: c.ExecType, attempt: int(c.Attempt), taskToken: token, stepDef: c.StepDef,
		runID: input.RunID, orgID: input.OrgID, projectID: input.ProjectID,
		repo: input.Repo, ref: input.Ref, commitSHA: input.CommitSHA,
		triggerType:   input.TriggerType,
		environment:   input.Environment,
		pipelineImage: input.PipelineImage,
		env:           merged,
		secretMapping: stepDef.Secrets,
		needsOutputs:  needsOutputs,
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
		// Route through the retry policy: a transient dispatch error (e.g. a DB
		// hiccup) backs off and retries like any failure, rather than terminally
		// failing the step. If attempts are exhausted it fails and advances so the
		// workflow doesn't wedge.
		l.failStepAndAdvance(ctx, c.WorkflowID, c.Name, int(c.Attempt), err)
		return
	}

	observe.StepsDispatched.Add(ctx, 1)
	// Record dispatch: stamps dispatched_at (so the undispatched sweep ignores this
	// step) and the executor handle (machine executor: assignment id) for
	// correlation. handle is empty for in-process executors (http/sim) — store NULL.
	var handlePtr *string
	if handle != "" {
		handlePtr = &handle
	}
	if err := q.MarkStepDispatched(ctx, db.MarkStepDispatchedParams{ID: c.ID, DispatchHandle: handlePtr}); err != nil {
		log.Warn().Err(err).Str("step", c.Name).Msg("engine: failed to record step dispatch")
	}
	dispatchMeta := map[string]any{}
	if handle != "" {
		dispatchMeta["handle"] = handle
	}
	emitStepEvent(ctx, q, stepTransition{
		workflowID: c.WorkflowID, stepName: c.Name, attempt: int(c.Attempt),
		eventType: "dispatched", to: stepRunning, from: stepRunning, metadata: dispatchMeta,
	})
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

	dispatchFailResult := StepResult{StepName: stepName, Success: false, Error: cause.Error()}
	if err := transitionStep(ctx, qtx, stepTransition{
		stepID: stepRow.ID, workflowID: workflowID, stepName: stepName, attempt: attempt,
		from: stepRow.Status, to: stepFailed, result: &dispatchFailResult,
		eventType: "dispatch_failed", reason: cause.Error(),
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
			tx.Rollback(ctx)
			continue
		}
		if err := tx.Commit(ctx); err != nil {
			tx.Rollback(ctx)
			continue
		}
		_ = q.NotifyEngine(ctx, wfID)
		l.engine.notifyState(ctx, wfID)
	}
}

// finishStaleWorkflows recovers workflows stuck 'running' with every step
// terminal — the finish was lost to a worker crash. It routes each through the
// real finishWorkflow path (finish the workflow, emit the workflow_finished
// event, FinishRun, enqueue webhooks) with the verdict derived from the steps,
// rather than the old bare UPDATE that stranded the run 'running' with no
// webhook and always recorded 'failed'. Claim and finish share one transaction
// so the FOR UPDATE ... SKIP LOCKED locks held by the claim protect the finish;
// a workflow a live advance is already finishing is skipped (locked) this tick.
func (l *Loop) finishStaleWorkflows(ctx context.Context) {
	tx, err := l.pool.Begin(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("engine: begin tx for stale-workflow finish")
		return
	}
	defer tx.Rollback(ctx)
	qtx := db.New(l.pool).WithTx(tx)

	stale, err := qtx.ClaimStaleWorkflows(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("engine: claim stale workflows failed")
		return
	}
	if len(stale) == 0 {
		return
	}
	for _, w := range stale {
		status := "succeeded"
		if w.HasFailure {
			status = "failed"
		}
		log.Warn().Str("workflow", w.ID).Str("status", status).
			Msg("engine: sweep recovered stale workflow (finishing)")
		finishWorkflow(ctx, qtx, w.ID, status)
	}
	if err := tx.Commit(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: commit stale-workflow finish failed")
		tx.Rollback(ctx)
		return
	}
	// Wake any waiting parents/webhook processing for the recovered workflows.
	q := db.New(l.pool)
	for _, w := range stale {
		_ = q.NotifyEngine(ctx, w.ID)
		l.engine.notifyState(ctx, w.ID)
	}
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
	l.finishStaleWorkflows(ctx)

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
	if err := q.DeleteStaleUnconsumedSignals(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: cleanup stale unconsumed signals failed")
	}
	if err := q.CleanupOldEngineEvents(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: cleanup old engine events failed")
	}
	// Runs retention: terminal runs past the window are deleted (workflows/
	// steps cascade). Batched — one batch per sweep keeps the delete bounded;
	// a backlog drains across sweeps.
	if days := l.config.runRetentionDays(); days > 0 {
		if n, err := q.DeleteOldRuns(ctx, int32(days)); err != nil {
			log.Warn().Err(err).Msg("engine: runs retention failed")
		} else if n > 0 {
			log.Info().Int64("count", n).Int("retentionDays", days).Msg("engine: runs retention pruned old runs")
		}
		// step_assignments do NOT cascade from runs (no FK on the hot dispatch
		// path), so prune the terminal ones on the same window — they're the
		// fattest unbounded table.
		if err := q.CleanupOldStepAssignments(ctx, int32(days)); err != nil {
			log.Warn().Err(err).Msg("engine: step-assignment retention failed")
		}
	}
	// Fleet append-only tables that grow forever otherwise (the engine sweep runs
	// in every worker, so this covers dispatch-only deployments too).
	if err := q.CleanupOldMachineEvents(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: machine-events retention failed")
	}
	if err := q.CleanupOldFleetDecisions(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: fleet-decisions retention failed")
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

		// Listener live — polling may back off to the idle cadence.
		l.notifyHealthy.Store(true)

		for {
			_, err := conn.Conn().WaitForNotification(ctx)
			if err != nil {
				// Listener down — fall back to tight polling until reconnected.
				l.notifyHealthy.Store(false)
				conn.Release()
				if ctx.Err() != nil {
					return
				}
				break // reconnect
			}
			// Wake both the tick loop (advancement) and the outbox delivery loop:
			// a committed state transition both queues work and may have enqueued
			// webhook events. Non-blocking — a coalesced wake is fine.
			select {
			case l.wake <- struct{}{}:
			default:
			}
			select {
			case l.outboxWake <- struct{}{}:
			default:
			}
		}
	}
}
