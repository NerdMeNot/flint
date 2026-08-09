package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// PgEngine is the Postgres-backed implementation of Engine.
type PgEngine struct {
	pool       db.Pool
	signingKey []byte // HMAC key for task tokens; empty = unsigned (dev/test)
	// stateObserver, if set, is called with a workflowID after each committed
	// state transition (step completion, cancellation) so an outer layer (the
	// server's SSE broadcaster) can push updates. The engine emits only an id —
	// it never imports the server — staying product-agnostic. Optional; nil = off.
	stateObserver func(ctx context.Context, workflowID string)
}

// SetStateObserver registers a callback invoked after each committed state
// transition. Not safe to call concurrently with engine operation; set it once
// at wiring time.
func (e *PgEngine) SetStateObserver(fn func(ctx context.Context, workflowID string)) {
	e.stateObserver = fn
}

func (e *PgEngine) notifyState(ctx context.Context, workflowID string) {
	if e.stateObserver != nil {
		e.stateObserver(ctx, workflowID)
	}
}

// New creates a new PgEngine. signingKey signs/verifies task tokens — it must
// match the key the worker loop uses to mint them, and must NOT be a value
// exposed to step pods (use the server-side JWT secret, not the internal token).
func New(pool db.Pool, signingKey []byte) *PgEngine {
	return &PgEngine{pool: pool, signingKey: signingKey}
}

func (e *PgEngine) Close() {}

// StartWorkflowWithWaves starts a workflow from an already-resolved DAG, with no
// forge or pipeline-YAML involvement. This is the product-neutral entry point:
// non-CI products (e.g. Flint Workflows) build their own [][]pipeline.Step waves
// and hand them to the engine, which executes them generically. Idempotent for
// root workflows, like StartWorkflow.
func (e *PgEngine) StartWorkflowWithWaves(ctx context.Context, input StartWorkflowInput, waves [][]pipeline.Step) (string, error) {
	return e.startWorkflow(ctx, input, waves, nil)
}

// StartWorkflowSeeded starts a workflow whose steps named in seed are pre-completed
// as 'succeeded' with their carried-over results (and their outputs folded into the
// workflow's step_outputs), so only the remaining steps execute. This is the engine
// primitive behind re-run-failed and retry-from-step: the caller decides which steps
// to carry over (every step NOT in seed runs fresh, gated normally by its deps).
func (e *PgEngine) StartWorkflowSeeded(ctx context.Context, input StartWorkflowInput, waves [][]pipeline.Step, seed map[string]StepResult) (string, error) {
	return e.startWorkflow(ctx, input, waves, seed)
}

func (e *PgEngine) startWorkflow(ctx context.Context, input StartWorkflowInput, waves [][]pipeline.Step, seed map[string]StepResult) (string, error) {
	logger := observe.Logger(ctx)

	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("engine: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(e.pool).WithTx(tx)

	runExists, err := qtx.RunExists(ctx, input.RunID)
	if err != nil {
		return "", fmt.Errorf("engine: check run: %w", err)
	}
	if !runExists {
		return "", fmt.Errorf("engine: pipeline_run %s not found", input.RunID)
	}

	if input.ParentWorkflowID == "" {
		if existingID, err := qtx.GetExistingWorkflow(ctx, input.RunID); err == nil {
			return existingID, nil // idempotent
		}
	}

	input.normalizeInputs()

	var parentID, parentStep *string
	if input.ParentWorkflowID != "" {
		parentID = &input.ParentWorkflowID
		parentStep = &input.ParentStepName
	}

	workflowID, err := qtx.InsertWorkflow(ctx, db.InsertWorkflowParams{
		RunID:      input.RunID,
		ParentID:   parentID,
		ParentStep: parentStep,
		Input:      mustJSON(input),
	})
	if err != nil {
		return "", fmt.Errorf("engine: insert workflow: %w", err)
	}

	// Cache the resolved DAG (wave→step-names) so advancement and queries work
	// the same as for CI. There is no pipeline YAML to store.
	if err := qtx.UpdateWorkflowPipeline(ctx, db.UpdateWorkflowPipelineParams{
		ID:          workflowID,
		PipelineDef: mustJSON(waves),
		DagWaves:    mustJSON(wavesToNames(waves)),
	}); err != nil {
		return "", fmt.Errorf("engine: update workflow: %w", err)
	}

	if err := createStepsAndAdvance(ctx, qtx, workflowID, waves, seed); err != nil {
		return "", err
	}

	wfID := workflowID
	if _, err := qtx.UpdateRunWorkflow(ctx, db.UpdateRunWorkflowParams{
		ID:         input.RunID,
		WorkflowID: &wfID,
	}); err != nil {
		return "", fmt.Errorf("engine: link pipeline_run: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("engine: commit: %w", err)
	}

	_ = db.New(e.pool).NotifyEngine(ctx, workflowID)
	logger.Info().Str("workflowID", workflowID).Int("waves", len(waves)).Msg("engine: workflow started (waves)")
	return workflowID, nil
}

// createStepsAndAdvance inserts step rows for the resolved waves and advances the
// workflow (queuing the initially-eligible steps). Shared by every product entry
// point. Steps named in seed are inserted already-succeeded with their carried-over
// result (re-run-failed / retry-from-step); seed is nil for a normal start.
func createStepsAndAdvance(ctx context.Context, qtx *db.Queries, workflowID string, waves [][]pipeline.Step, seed map[string]StepResult) error {
	seededOutputs := make(map[string]StepResult)
	for waveIdx, wave := range waves {
		for _, step := range wave {
			p := buildInsertStepParams(workflowID, waveIdx, step)

			if res, ok := seed[step.Name]; ok && res.Success {
				if err := qtx.InsertSeededStep(ctx, db.InsertSeededStepParams{
					WorkflowID:           p.WorkflowID,
					Name:                 p.Name,
					ExecType:             p.ExecType,
					Wave:                 p.Wave,
					MaxAttempts:          p.MaxAttempts,
					StepDef:              p.StepDef,
					TimeoutSeconds:       p.TimeoutSeconds,
					RetryBackoff:         p.RetryBackoff,
					RetryIntervalSeconds: p.RetryIntervalSeconds,
					OnFailure:            p.OnFailure,
					Result:               mustJSON(res),
				}); err != nil {
					return fmt.Errorf("engine: insert seeded step %s: %w", step.Name, err)
				}
				seededOutputs[step.Name] = res
				emitStepEvent(ctx, qtx, stepTransition{
					workflowID: workflowID, stepName: step.Name, attempt: 0,
					to: stepSucceeded, eventType: "seeded", actor: actorEngine,
					reason: "carried over from prior run",
				})
				continue
			}

			if err := qtx.InsertStep(ctx, p); err != nil {
				return fmt.Errorf("engine: insert step %s: %w", step.Name, err)
			}
		}
	}

	// Persist carried-over outputs so downstream `dependsOn` gating and
	// steps.<name>.<output> expressions resolve against the seeded results.
	if len(seededOutputs) > 0 {
		if err := qtx.UpdateStepOutputs(ctx, db.UpdateStepOutputsParams{
			ID:      workflowID,
			Column2: mustJSON(seededOutputs),
		}); err != nil {
			return fmt.Errorf("engine: seed step outputs: %w", err)
		}
	}

	if err := advanceWorkflow(ctx, qtx, workflowID, 0); err != nil {
		return fmt.Errorf("engine: advance: %w", err)
	}
	return nil
}

// buildInsertStepParams derives the persisted step row from a pipeline.Step,
// resolving timeout, retry policy, and onFailure from the step definition.
func buildInsertStepParams(workflowID string, waveIdx int, step pipeline.Step) db.InsertStepParams {
	timeoutSec := 7200
	if step.Timeout != "" {
		if d, parseErr := time.ParseDuration(step.Timeout); parseErr == nil {
			timeoutSec = int(d.Seconds())
		}
	}
	maxAttempts := 1
	retryBackoff := "exponential"
	retryIntervalSec := 5
	onFailure := "fail"

	if step.ContinueOnError {
		onFailure = "continue"
	}
	if step.Retry != nil {
		if step.Retry.Attempts > 0 {
			maxAttempts = step.Retry.Attempts
		}
		if step.Retry.Delay != "" {
			if d, parseErr := time.ParseDuration(step.Retry.Delay); parseErr == nil {
				retryIntervalSec = int(d.Seconds())
			}
		}
	}

	return db.InsertStepParams{
		WorkflowID:           workflowID,
		Name:                 step.Name,
		ExecType:             step.ExecType(),
		Wave:                 int32(waveIdx),
		MaxAttempts:          int32(maxAttempts),
		StepDef:              mustJSON(step),
		TimeoutSeconds:       int32(timeoutSec),
		RetryBackoff:         retryBackoff,
		RetryIntervalSeconds: int32(retryIntervalSec),
		OnFailure:            onFailure,
	}
}

// CompleteStep reports that a step has finished.
// Idempotent: calling twice with the same token for a terminal step returns nil.
// The transaction body is wrapped in retryOnConflict so a transient deadlock or
// serialization failure (e.g. racing a CancelWorkflow on a parent) self-heals
// rather than surfacing to the caller.
func (e *PgEngine) CompleteStep(ctx context.Context, encodedToken string, result StepResult) error {
	token, err := DecodeTaskToken(encodedToken, e.signingKey)
	if err != nil {
		return err
	}
	return retryOnConflict(ctx, func() error {
		return e.completeStepOnce(ctx, token, result)
	})
}

func (e *PgEngine) completeStepOnce(ctx context.Context, token TaskToken, result StepResult) error {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("engine: begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(e.pool).WithTx(tx)

	// Lock and validate the step.
	stepRow, err := qtx.LockStep(ctx, db.LockStepParams{
		WorkflowID: token.WorkflowID,
		Name:       token.StepName,
		Attempt:    int32(token.Attempt),
	})
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil
		}
		return fmt.Errorf("engine: query step: %w", err)
	}

	// Idempotency: already terminal → success.
	if isTerminal(stepRow.Status) {
		return nil
	}

	newStatus := stepSucceeded
	if !result.Success {
		newStatus = stepFailed
	}
	observe.StepsCompleted.Add(ctx, 1, metric.WithAttributes(attribute.String("status", newStatus)))

	// Update step through the transition chokepoint (validates, updates the row,
	// records a history event). The agent (or in-process executor) is the actor.
	if err := transitionStep(ctx, qtx, stepTransition{
		stepID:     stepRow.ID,
		workflowID: token.WorkflowID,
		stepName:   token.StepName,
		attempt:    token.Attempt,
		from:       stepRow.Status,
		to:         newStatus,
		result:     &result,
		actor:      actorAgent,
	}); err != nil {
		return fmt.Errorf("engine: complete step transition: %w", err)
	}

	// Cancel timeout timer.
	if err := qtx.CancelTimer(ctx, db.CancelTimerParams{
		WorkflowID: token.WorkflowID,
		StepName:   token.StepName,
		TimerType:  timerTimeout,
	}); err != nil {
		log.Warn().Err(err).Str("step", token.StepName).Msg("engine: failed to cancel timeout timer")
	}

	// Update workflow step_outputs.
	if err := qtx.UpdateStepOutputs(ctx, db.UpdateStepOutputsParams{
		ID:      token.WorkflowID,
		Column2: mustJSON(map[string]StepResult{token.StepName: result}),
	}); err != nil {
		return fmt.Errorf("engine: update step_outputs: %w", err)
	}

	// Schedule a backoff retry if the step failed and attempts remain.
	if !result.Success {
		if _, err := maybeScheduleRetry(ctx, qtx, stepRow.ID, token.WorkflowID, token.StepName,
			token.Attempt, int(stepRow.MaxAttempts), stepRow.RetryBackoff, int(stepRow.RetryIntervalSeconds)); err != nil {
			return err
		}
	}

	// Advance the workflow.
	if err := advanceWorkflow(ctx, qtx, token.WorkflowID, 0); err != nil {
		return fmt.Errorf("engine: advance: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("engine: commit: %w", err)
	}

	_ = db.New(e.pool).NotifyEngine(ctx, token.WorkflowID)
	e.notifyState(ctx, token.WorkflowID)
	return nil
}

// maybeScheduleRetry creates the next attempt (parked in retry_wait) and a
// retry_backoff timer when attempts remain. Shared by step-reported failures
// (CompleteStep) and dispatch failures (failStepAndAdvance) so both honour the
// retry policy and its backoff. Returns whether a retry was scheduled. The caller
// must already have marked the current attempt failed and runs inside a tx.
func maybeScheduleRetry(ctx context.Context, qtx *db.Queries, stepID, workflowID, stepName string,
	attempt, maxAttempts int, retryBackoff string, retryIntervalSec int) (bool, error) {
	if attempt+1 >= maxAttempts {
		return false, nil
	}
	if err := qtx.CreateRetryStep(ctx, db.CreateRetryStepParams{
		NewAttempt:   int32(attempt + 1),
		SourceStepID: stepID,
	}); err != nil {
		return false, fmt.Errorf("engine: create retry step: %w", err)
	}
	backoff := backoffDuration(attempt, retryBackoff, retryIntervalSec)
	if err := qtx.UpsertTimer(ctx, db.UpsertTimerParams{
		WorkflowID: workflowID,
		StepName:   stepName,
		TimerType:  timerRetryBackoff,
		Secs:       backoff.Seconds(),
	}); err != nil {
		return false, fmt.Errorf("engine: create retry timer: %w", err)
	}
	// Record the retry on the freshly-parked attempt (retry_wait) for the timeline.
	emitStepEvent(ctx, qtx, stepTransition{
		workflowID: workflowID,
		stepName:   stepName,
		attempt:    attempt + 1,
		eventType:  "retry_scheduled",
		to:         stepRetryWait,
		actor:      actorEngine,
		metadata:   map[string]any{"backoffSeconds": int(backoff.Seconds()), "ofMaxAttempts": maxAttempts},
	})
	return true, nil
}

// DeliverSignal writes a signal for a workflow.
func (e *PgEngine) DeliverSignal(ctx context.Context, workflowID, signalName string, payload any) error {
	payloadJSON := mustJSON(payload)
	q := db.New(e.pool)
	err := q.InsertSignal(ctx, db.InsertSignalParams{
		WorkflowID: workflowID,
		SignalName: signalName,
		Payload:    payloadJSON,
	})
	if err != nil {
		return fmt.Errorf("engine: insert signal: %w", err)
	}
	_ = q.NotifyEngine(ctx, workflowID)
	return nil
}

// CancelWorkflow marks a workflow and all non-terminal steps as cancelled.
// Wrapped in retryOnConflict: cancel locks the workflow tree parent→child while a
// concurrent child CompleteStep locks child→parent, so a deadlock is possible;
// Postgres aborts one and we transparently retry.
func (e *PgEngine) CancelWorkflow(ctx context.Context, workflowID string) error {
	return retryOnConflict(ctx, func() error {
		return e.cancelWorkflowOnce(ctx, workflowID)
	})
}

func (e *PgEngine) cancelWorkflowOnce(ctx context.Context, workflowID string) error {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(e.pool).WithTx(tx)

	if err := qtx.CancelWorkflow(ctx, workflowID); err != nil {
		return fmt.Errorf("engine: cancel workflow: %w", err)
	}
	emitWorkflowEvent(ctx, qtx, workflowTransition{
		workflowID: workflowID,
		to:         wfCancelled,
		eventType:  "workflow_cancelled",
		actor:      actorEngine,
	})
	cancelled, err := qtx.CancelPendingSteps(ctx, workflowID)
	if err != nil {
		log.Error().Err(err).Str("workflowID", workflowID).Msg("engine: failed to cancel pending steps")
	}
	for _, st := range cancelled {
		emitStepEvent(ctx, qtx, stepTransition{
			workflowID: workflowID,
			stepName:   st.Name,
			attempt:    int(st.Attempt),
			from:       st.OldStatus,
			to:         stepCancelled,
			actor:      actorEngine,
		})
	}
	if err := qtx.CancelAllWorkflowTimers(ctx, workflowID); err != nil {
		log.Error().Err(err).Str("workflowID", workflowID).Msg("engine: failed to cancel timers")
	}
	if err := qtx.CancelChildWorkflows(ctx, &workflowID); err != nil {
		log.Error().Err(err).Str("workflowID", workflowID).Msg("engine: failed to cancel child workflows")
	}
	// Mark the run cancelled too (workflow cancel alone left the run row stale).
	// This also makes the run eligible for executor cleanup (cleaned_at IS NULL
	// + terminal status), so the loop tears down its pods on the next tick.
	if err := qtx.FinishRun(ctx, db.FinishRunParams{WorkflowID: &workflowID, Status: "cancelled"}); err != nil {
		log.Error().Err(err).Str("workflowID", workflowID).Msg("engine: failed to mark run cancelled")
	}

	if err := tx.Commit(ctx); err != nil {
		return err
	}
	// Wake the loop so cleanup runs promptly rather than at the next poll/sweep.
	_ = db.New(e.pool).NotifyEngine(ctx, workflowID)
	e.notifyState(ctx, workflowID)
	return nil
}

// PauseWorkflow transitions a running workflow to paused. No new steps are
// claimed (ClaimQueuedSteps filters to running workflows) or queued
// (advanceWorkflow no-ops unless running) until ResumeWorkflow. In-flight steps
// still finish and record results.
func (e *PgEngine) PauseWorkflow(ctx context.Context, workflowID string) error {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(e.pool).WithTx(tx)

	n, err := qtx.PauseWorkflow(ctx, workflowID)
	if err != nil {
		return fmt.Errorf("engine: pause workflow: %w", err)
	}
	if n == 0 {
		return nil // not running — idempotent no-op
	}
	if err := qtx.SetRunStatusByWorkflow(ctx, db.SetRunStatusByWorkflowParams{
		WorkflowID: &workflowID, Status: "paused",
	}); err != nil {
		log.Warn().Err(err).Str("workflow", workflowID).Msg("engine: failed to mark run paused")
	}
	emitWorkflowEvent(ctx, qtx, workflowTransition{
		workflowID: workflowID, from: wfRunning, to: wfPaused, eventType: "paused",
	})
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	e.notifyState(ctx, workflowID)
	return nil
}

// ResumeWorkflow transitions a paused workflow back to running and advances it so
// steps that became eligible while paused are queued.
func (e *PgEngine) ResumeWorkflow(ctx context.Context, workflowID string) error {
	return retryOnConflict(ctx, func() error {
		tx, err := e.pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx) //nolint:errcheck
		qtx := db.New(e.pool).WithTx(tx)

		n, err := qtx.ResumeWorkflow(ctx, workflowID)
		if err != nil {
			return fmt.Errorf("engine: resume workflow: %w", err)
		}
		if n == 0 {
			return nil // not paused — idempotent no-op
		}
		if err := qtx.SetRunStatusByWorkflow(ctx, db.SetRunStatusByWorkflowParams{
			WorkflowID: &workflowID, Status: "running",
		}); err != nil {
			log.Warn().Err(err).Str("workflow", workflowID).Msg("engine: failed to mark run running")
		}
		emitWorkflowEvent(ctx, qtx, workflowTransition{
			workflowID: workflowID, from: wfPaused, to: wfRunning, eventType: "resumed",
		})
		if err := advanceWorkflow(ctx, qtx, workflowID, 0); err != nil {
			return fmt.Errorf("engine: advance after resume: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		_ = db.New(e.pool).NotifyEngine(ctx, workflowID)
		e.notifyState(ctx, workflowID)
		return nil
	})
}

// ResolveStepManually forces a non-terminal step to a terminal outcome
// (succeeded, failed, or skipped) on operator command — the escape hatch for a
// step wedged 'running'/'waiting' that an executor will never complete. It records
// a manual_resolve history event attributing the actor and reason, then advances
// the workflow. A real completion arriving later for the same step hits the
// terminal-idempotency guard in CompleteStep and is a safe no-op.
func (e *PgEngine) ResolveStepManually(ctx context.Context, workflowID, stepName, outcome, actor, reason string) error {
	switch outcome {
	case stepSucceeded, stepFailed, stepSkipped:
	default:
		return fmt.Errorf("engine: invalid manual outcome %q (want succeeded|failed|skipped)", outcome)
	}
	return retryOnConflict(ctx, func() error {
		tx, err := e.pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx) //nolint:errcheck
		qtx := db.New(e.pool).WithTx(tx)

		st, err := qtx.LockLatestStep(ctx, db.LockLatestStepParams{WorkflowID: workflowID, Name: stepName})
		if err != nil {
			if err == pgx.ErrNoRows {
				return fmt.Errorf("engine: step %q not found", stepName)
			}
			return fmt.Errorf("engine: lock step: %w", err)
		}
		if isTerminal(st.Status) {
			return fmt.Errorf("engine: step %q already %s", stepName, st.Status)
		}

		result := StepResult{StepName: stepName, Success: outcome == stepSucceeded}
		if outcome != stepSucceeded && reason != "" {
			result.Error = reason
		}
		var resultPtr *StepResult
		if outcome != stepSkipped {
			resultPtr = &result
		}
		if err := transitionStep(ctx, qtx, stepTransition{
			stepID: st.ID, workflowID: workflowID, stepName: stepName, attempt: int(st.Attempt),
			from: st.Status, to: outcome, result: resultPtr, eventType: "manual_resolve",
			actor: actorOperator(actor), reason: reason, force: true,
		}); err != nil {
			return err
		}
		// A manually-succeeded step may feed downstream deps/expressions.
		if outcome == stepSucceeded {
			if err := qtx.UpdateStepOutputs(ctx, db.UpdateStepOutputsParams{
				ID:      workflowID,
				Column2: mustJSON(map[string]StepResult{stepName: result}),
			}); err != nil {
				return fmt.Errorf("engine: record manual step outputs: %w", err)
			}
		}
		// Cancel any pending timeout/gate/wait timer for the step.
		if err := qtx.CancelTimer(ctx, db.CancelTimerParams{
			WorkflowID: workflowID, StepName: stepName, TimerType: timerTimeout,
		}); err != nil {
			log.Warn().Err(err).Str("step", stepName).Msg("engine: failed to cancel timer on manual resolve")
		}
		if err := advanceWorkflow(ctx, qtx, workflowID, 0); err != nil {
			return fmt.Errorf("engine: advance after manual resolve: %w", err)
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		_ = db.New(e.pool).NotifyEngine(ctx, workflowID)
		e.notifyState(ctx, workflowID)
		return nil
	})
}

// QueryWorkflow returns the current state of a workflow.
func (e *PgEngine) QueryWorkflow(ctx context.Context, workflowID string) (*WorkflowState, error) {
	q := db.New(e.pool)
	ws, err := q.GetWorkflowStatus(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("engine: query workflow: %w", err)
	}
	result := &WorkflowState{
		WorkflowID: workflowID,
		RunID:      ws.RunID,
		Status:     ws.Status,
		StartedAt:  ws.StartedAt,
		FinishedAt: ws.FinishedAt,
	}

	stepRows, err := q.ListStepsByWorkflow(ctx, workflowID)
	if err != nil {
		return nil, fmt.Errorf("engine: query steps: %w", err)
	}
	for _, row := range stepRows {
		s := StepState{
			Name:        row.Name,
			Status:      row.Status,
			ExecType:    row.ExecType,
			Wave:        int(row.Wave),
			Attempt:     int(row.Attempt),
			MaxAttempts: int(row.MaxAttempts),
			ScheduledAt: row.QueuedAt,
			StartedAt:   row.StartedAt,
			FinishedAt:  row.FinishedAt,
		}
		if row.Result != nil {
			var r StepResult
			if json.Unmarshal(row.Result, &r) == nil {
				s.ExitCode = &r.ExitCode
				s.Error = r.Error
			}
		}
		if row.DependsOn != nil {
			// DependsOn comes from JSONB via pgx as interface{} (typically []interface{}).
			// Round-trip through JSON to get []string.
			raw, _ := json.Marshal(row.DependsOn)
			_ = json.Unmarshal(raw, &s.DependsOn)
		}
		result.Steps = append(result.Steps, s)
	}
	return result, nil
}

func finishWorkflow(ctx context.Context, qtx *db.Queries, workflowID, status string) {
	if err := qtx.FinishWorkflow(ctx, db.FinishWorkflowParams{ID: workflowID, Status: status}); err != nil {
		log.Error().Err(err).Str("workflowID", workflowID).Str("status", status).
			Msg("engine: failed to finish workflow (sweep will recover)")
	}
	emitWorkflowEvent(ctx, qtx, workflowTransition{
		workflowID: workflowID,
		from:       wfRunning,
		to:         status,
		eventType:  "workflow_finished",
		actor:      actorEngine,
	})
	if err := qtx.FinishRun(ctx, db.FinishRunParams{WorkflowID: &workflowID, Status: status}); err != nil {
		log.Error().Err(err).Str("workflowID", workflowID).Str("status", status).
			Msg("engine: failed to finish run")
	}
	// Webhook delivery is decoupled via the outbox. We look up the runID from
	// the workflow and enqueue events. This runs inside the same transaction as
	// finishWorkflow — if the tx rolls back, no webhooks are enqueued.
	ws, err := qtx.GetWorkflowStatus(ctx, workflowID)
	if err == nil {
		// Insert outbox events inline — they'll be delivered by processOutbox
		// on the next tick. The idempotency key prevents duplicates if this
		// path runs more than once (e.g., sweep retry).
		enqueueWebhooksInTx(ctx, qtx, ws.RunID, status)
	}
}

func backoffDuration(attempt int, policy string, baseSeconds int) time.Duration {
	var base time.Duration
	switch policy {
	case "exponential":
		base = time.Duration(baseSeconds*(1<<uint(attempt))) * time.Second
	case "linear":
		base = time.Duration(baseSeconds*(attempt+1)) * time.Second
	default:
		base = time.Duration(baseSeconds) * time.Second
	}
	// Add ±20% jitter to prevent thundering herd.
	jitter := 0.8 + 0.4*rand.Float64()
	return time.Duration(float64(base) * jitter)
}

func wavesToNames(waves [][]pipeline.Step) [][]string {
	result := make([][]string, len(waves))
	for i, wave := range waves {
		names := make([]string, len(wave))
		for j, s := range wave {
			names[j] = s.Name
		}
		result[i] = names
	}
	return result
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		// All types passed to mustJSON are plain Go structs with JSON tags.
		// A marshal failure here indicates a programming error (e.g. a channel
		// or func field was added to a struct), not a runtime condition.
		// Log at fatal level — this kills the process, which is preferable to
		// silently writing null into a JSONB column and corrupting workflow state.
		log.Fatal().Err(err).Str("type", fmt.Sprintf("%T", v)).Msg("engine: mustJSON: cannot marshal")
		return nil // unreachable
	}
	return b
}
