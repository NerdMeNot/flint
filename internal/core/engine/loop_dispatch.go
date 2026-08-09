package engine

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/observe"
)

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
	defer tx.Rollback(ctx) //nolint:errcheck
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
	defer tx.Rollback(ctx) //nolint:errcheck
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
