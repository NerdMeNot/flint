package engine

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/observe"
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

	// Phase 2: Process gate approval and rejection signals.
	l.processSignals(ctx)
	l.processRejections(ctx)

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

	// For each claimed step, generate task token, fetch workflow context, dispatch.
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

		// Fetch workflow input for context.
		inputJSON, err := q.GetWorkflowInput(ctx, c.WorkflowID)
		if err != nil {
			log.Error().Err(err).Str("step", c.Name).Msg("engine: failed to fetch workflow input")
			continue
		}
		var input StartWorkflowInput
		if json.Unmarshal(inputJSON, &input) != nil {
			log.Error().Str("step", c.Name).Msg("engine: failed to unmarshal workflow input")
			continue
		}

		// Create timers for gate steps, respecting per-step timeout if set.
		if c.ExecType == "gate" {
			var gateStep struct {
				Timeout string `json:"timeout"`
			}
			_ = json.Unmarshal(c.StepDef, &gateStep)
			l.createStepTimers(ctx, c.WorkflowID, c.Name, gateStep.Timeout)
		}

		// Dispatch run/use/steps steps as K8s Jobs.
		if c.Status == "running" && len(l.executors) > 0 {
			// Enforce per-org concurrency limit: if the org already has too
			// many running steps, push this one back to 'queued'. It'll be
			// picked up on the next tick when a slot opens.
			if input.OrgID != "" {
				if throttled := l.checkConcurrencyLimit(ctx, q, input.OrgID, c.ID); throttled {
					log.Debug().Str("step", c.Name).Str("org", input.OrgID).
						Msg("engine: org concurrency limit reached, re-queuing step")
					observe.StepsThrottled.Add(ctx, 1, metric.WithAttributes(attribute.String("org_id", input.OrgID)))
					continue
				}
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
				// No executor for this step type (e.g. DB-only mode) — leave the
				// step running; it'll be picked up if an executor appears.
				continue
			}
			if err != nil {
				log.Error().Err(err).Str("step", c.Name).Msg("engine: dispatch failed")
				observe.DispatchErrors.Add(ctx, 1, metric.WithAttributes(attribute.String("step", c.Name)))
				// Mark the step failed AND advance the workflow in one tx, so the
				// failure propagates to downstream steps. Without the advance the
				// workflow would wedge: the failed step is terminal, downstream
				// steps stay pending, and no sweep path recovers it (sweep only
				// rescues 'running' steps past deadline).
				l.failStepAndAdvance(ctx, c.ID, c.WorkflowID, c.Name, err)
			} else {
				observe.StepsDispatched.Add(ctx, 1)
				// Record the executor's handle (e.g. k8s Job name) for correlation.
				if handle != "" {
					if err := q.SetStepK8sJobName(ctx, db.SetStepK8sJobNameParams{
						ID:         c.ID,
						K8sJobName: &handle,
					}); err != nil {
						log.Warn().Err(err).Str("step", c.Name).Msg("engine: failed to record step handle")
					}
				}
			}
		}
	}
}

// failStepAndAdvance marks a step failed and advances the workflow in a single
// transaction, then wakes the loop. Used when dispatch fails — the workflow must
// progress (run onFailure steps, skip downstream, or finish) rather than wedge.
func (l *Loop) failStepAndAdvance(ctx context.Context, stepID, workflowID, stepName string, cause error) {
	tx, err := l.pool.Begin(ctx)
	if err != nil {
		log.Error().Err(err).Str("step", stepName).Msg("engine: failStepAndAdvance: begin tx")
		return
	}
	defer tx.Rollback(ctx)
	qtx := db.New(l.pool).WithTx(tx)

	if err := qtx.UpdateStepResult(ctx, db.UpdateStepResultParams{
		ID:     stepID,
		Status: "failed",
		Result: mustJSON(StepResult{StepName: stepName, Success: false, Error: cause.Error()}),
	}); err != nil {
		log.Error().Err(err).Str("step", stepName).Msg("engine: failed to mark step failed after dispatch error")
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

// createStepTimers creates timers for gate steps.
// stepTimeout is the raw timeout string from the step definition (e.g. "1h",
// "30m"). If empty or unparseable, defaults to 4 hours.
func (l *Loop) createStepTimers(ctx context.Context, workflowID, stepName, stepTimeout string) {
	q := db.New(l.pool)

	timeout := 4 * time.Hour
	if stepTimeout != "" {
		if d, err := time.ParseDuration(stepTimeout); err == nil && d > 0 {
			timeout = d
		}
	}
	if err := q.CreateTimer(ctx, db.CreateTimerParams{
		WorkflowID: workflowID,
		StepName:   stepName,
		TimerType:  "gate_timeout",
		Secs:       timeout.Seconds(),
	}); err != nil {
		log.Error().Err(err).Str("step", stepName).Dur("timeout", timeout).
			Msg("engine: failed to create gate timeout timer")
	}
}

// processSignals checks for gate approval signals on waiting steps.
func (l *Loop) processSignals(ctx context.Context) {
	q := db.New(l.pool)
	approvals, err := q.ListWaitingGatesWithSignals(ctx)
	if err != nil {
		return
	}

	for _, a := range approvals {
		tx, err := l.pool.Begin(ctx)
		if err != nil {
			continue
		}
		qtx := db.New(l.pool).WithTx(tx)

		if err := qtx.ConsumeSignal(ctx, a.SignalID); err != nil {
			log.Error().Err(err).Str("step", a.StepName).Msg("engine: failed to consume approval signal")
		}
		if err := qtx.UpdateStepResult(ctx, db.UpdateStepResultParams{
			ID:     a.StepID,
			Status: "succeeded",
			Result: mustJSON(StepResult{StepName: a.StepName, Success: true}),
		}); err != nil {
			log.Error().Err(err).Str("step", a.StepName).Msg("engine: failed to approve gate step")
			tx.Rollback(ctx)
			continue
		}
		if err := qtx.CancelTimer(ctx, db.CancelTimerParams{
			WorkflowID: a.WorkflowID,
			StepName:   a.StepName,
			TimerType:  "gate_timeout",
		}); err != nil {
			log.Warn().Err(err).Str("step", a.StepName).Msg("engine: failed to cancel gate timer")
		}
		if err := advanceWorkflow(ctx, qtx, a.WorkflowID, 0); err != nil {
			log.Error().Err(err).Str("workflow", a.WorkflowID).Msg("engine: failed to advance after gate approval")
		}

		if err := tx.Commit(ctx); err != nil {
			tx.Rollback(ctx)
			log.Error().Err(err).Str("step", a.StepName).Msg("engine: failed to commit gate approval")
		} else {
			log.Info().Str("step", a.StepName).Msg("engine: gate approved")
		}
	}
}

// processRejections checks for gate rejection signals on waiting steps.
// When a gate is rejected, the step is marked as 'failed' with the rejection
// reason, and the workflow is advanced so that `when: onFailure` steps can run.
func (l *Loop) processRejections(ctx context.Context) {
	q := db.New(l.pool)
	rejections, err := q.ListWaitingGatesWithRejectSignals(ctx)
	if err != nil {
		return
	}

	for _, r := range rejections {
		tx, err := l.pool.Begin(ctx)
		if err != nil {
			continue
		}
		qtx := db.New(l.pool).WithTx(tx)

		// Extract rejection reason from signal payload.
		reason := "rejected"
		var payload struct {
			Reason string `json:"reason"`
		}
		if json.Unmarshal(r.Payload, &payload) == nil && payload.Reason != "" {
			reason = "rejected: " + payload.Reason
		}

		if err := qtx.ConsumeSignal(ctx, r.SignalID); err != nil {
			log.Error().Err(err).Str("step", r.StepName).Msg("engine: failed to consume rejection signal")
		}
		if err := qtx.UpdateStepResult(ctx, db.UpdateStepResultParams{
			ID:     r.StepID,
			Status: "failed",
			Result: mustJSON(StepResult{StepName: r.StepName, Success: false, Error: reason}),
		}); err != nil {
			log.Error().Err(err).Str("step", r.StepName).Msg("engine: failed to reject gate step")
			tx.Rollback(ctx)
			continue
		}
		if err := qtx.CancelTimer(ctx, db.CancelTimerParams{
			WorkflowID: r.WorkflowID,
			StepName:   r.StepName,
			TimerType:  "gate_timeout",
		}); err != nil {
			log.Warn().Err(err).Str("step", r.StepName).Msg("engine: failed to cancel gate timer")
		}
		if err := advanceWorkflow(ctx, qtx, r.WorkflowID, 0); err != nil {
			log.Error().Err(err).Str("workflow", r.WorkflowID).Msg("engine: failed to advance after gate rejection")
		}

		if err := tx.Commit(ctx); err != nil {
			tx.Rollback(ctx)
			log.Error().Err(err).Str("step", r.StepName).Msg("engine: failed to commit gate rejection")
		} else {
			log.Info().Str("step", r.StepName).Str("reason", reason).Msg("engine: gate rejected")
		}
	}
}

// sweep detects and recovers from stale state.
func (l *Loop) sweep(ctx context.Context) {
	log.Debug().Msg("engine: sweep started")

	// 1. Stale running steps past their deadline.
	q := db.New(l.pool)
	count, err := q.SweepStaleRunningSteps(ctx)
	if err == nil && count > 0 {
		log.Warn().Int64("count", count).Msg("engine: sweep recovered stale steps")

		wfIDs, wfErr := q.RecentlyFailedWorkflowIDs(ctx)
		if wfErr != nil {
			log.Warn().Err(wfErr).Msg("engine: sweep failed to list recently failed workflows")
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

	// 2. Stale workflows where all steps are terminal but workflow still "running".
	if err := q.SweepStaleWorkflows(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: sweep stale workflows failed")
	}

	// 3. Clean up fired timers older than 1 hour, resolved outbox events (>7d),
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

	// 4. Tear down resources for terminal runs not yet cleaned. Same exactly-once
	// path as the per-tick cleanup; kept here as a backstop in case a run reached
	// a terminal state without a following tick.
	l.cleanupFinishedRuns(ctx)

	log.Debug().Msg("engine: sweep completed")
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

// checkConcurrencyLimit checks if the org has hit its concurrent step limit.
// If at limit, reverts the step to 'queued' and returns true (throttled).
// Returns false if the step can proceed.
func (l *Loop) checkConcurrencyLimit(ctx context.Context, q *db.Queries, orgID, stepID string) bool {
	limit, err := q.GetOrgConcurrencyLimit(ctx, orgID)
	if err != nil {
		return false // can't check — allow dispatch
	}

	running, err := q.CountRunningStepsByOrg(ctx, orgID)
	if err != nil {
		return false
	}

	if running >= int64(limit) {
		// Re-queue: reset status back to 'queued' so it's picked up next tick.
		if err := q.SetStepQueued(ctx, stepID); err != nil {
			log.Error().Err(err).Str("stepID", stepID).Msg("engine: failed to re-queue throttled step")
		}
		return true
	}
	return false
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
