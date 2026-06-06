package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"path"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// PgEngine is the Postgres-backed implementation of Engine.
type PgEngine struct {
	pool       db.Pool
	files      FileGetter // fetches pipeline + template files
	signingKey []byte     // HMAC key for task tokens; empty = unsigned (dev/test)
}

// New creates a new PgEngine. files supplies pipeline + template fetching;
// signingKey signs/verifies task tokens — it must match the key the worker loop
// uses to mint them, and must NOT be a value exposed to step pods (use the
// server-side JWT secret, not the internal token).
func New(pool db.Pool, files FileGetter, signingKey []byte) *PgEngine {
	return &PgEngine{pool: pool, files: files, signingKey: signingKey}
}

func (e *PgEngine) Close() {}

// StartWorkflow creates a complete workflow execution in a single transaction.
// Idempotent: if a root workflow for this runID already exists, returns its ID.
func (e *PgEngine) StartWorkflow(ctx context.Context, input StartWorkflowInput) (string, error) {
	logger := observe.Logger(ctx)

	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("engine: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	qtx := db.New(e.pool).WithTx(tx)

	// Verify pipeline_runs exists.
	runExists, err := qtx.RunExists(ctx, input.RunID)
	if err != nil {
		return "", fmt.Errorf("engine: check run: %w", err)
	}
	if !runExists {
		return "", fmt.Errorf("engine: pipeline_run %s not found", input.RunID)
	}

	// Duplicate check for root workflows (child workflows can share run_id).
	if input.ParentWorkflowID == "" {
		existingID, err := qtx.GetExistingWorkflow(ctx, input.RunID)
		if err == nil {
			return existingID, nil // idempotent
		}
	}

	// Create workflow row. Synthesize the generic Inputs namespaces (git/run)
	// so the engine's expression context reads from Inputs, not typed fields.
	input.normalizeInputs()
	inputJSON := mustJSON(input)
	var parentID, parentStep *string
	if input.ParentWorkflowID != "" {
		parentID = &input.ParentWorkflowID
		parentStep = &input.ParentStepName
	}

	workflowID, err := qtx.InsertWorkflow(ctx, db.InsertWorkflowParams{
		RunID:      input.RunID,
		ParentID:   parentID,
		ParentStep: parentStep,
		Input:      inputJSON,
	})
	if err != nil {
		return "", fmt.Errorf("engine: insert workflow: %w", err)
	}

	// Fetch pipeline YAML — on failure, rollback entirely (don't pollute DB).
	filePath := path.Join(input.PipelinePath, input.WorkflowFile)
	rawYAML, err := e.files.GetFile(ctx, input.Repo, input.CommitSHA, filePath)
	if err != nil {
		logger.Error().Err(err).Str("file", filePath).Msg("engine: failed to fetch pipeline")
		return "", fmt.Errorf("engine: fetch pipeline: %w", err)
	}

	p, err := pipeline.Parse(rawYAML)
	if err != nil {
		logger.Error().Err(err).Msg("engine: failed to parse pipeline")
		return "", fmt.Errorf("engine: parse pipeline: %w", err)
	}

	// Capture pipeline-level defaults for dispatch-time fallback.
	input.PipelineImage = p.Image
	input.PipelineServiceAccount = p.ServiceAccount

	// Resolve templates (use: → run/steps).
	resolver := &fileResolver{
		files:        e.files,
		repo:         input.Repo,
		ref:          input.CommitSHA,
		pipelinePath: input.PipelinePath,
	}
	p, err = pipeline.ResolveTemplates(ctx, p, resolver)
	if err != nil {
		logger.Error().Err(err).Msg("engine: failed to resolve templates")
		return "", fmt.Errorf("engine: resolve templates: %w", err)
	}

	// Expand matrix steps into individual variants.
	p = pipeline.ExpandMatrix(p)

	// Coalesce consecutive same-runner steps into shared pods when the pipeline
	// declares a top-level runner. This eliminates workspace sync overhead for
	// simple sequential pipelines.
	p = pipeline.CoalesceSteps(p)

	// Resolve DAG — filter by environment if set.
	var waves [][]pipeline.Step
	if input.Environment != "" {
		waves, err = pipeline.ResolveDagForEnv(p, input.Environment)
	} else {
		waves, err = pipeline.ResolveDag(p)
	}
	if err != nil {
		logger.Error().Err(err).Msg("engine: failed to resolve DAG")
		return "", fmt.Errorf("engine: resolve DAG: %w", err)
	}

	// Cache parsed pipeline on workflow row.
	dagWaveNames := wavesToNames(waves)
	err = qtx.UpdateWorkflowPipeline(ctx, db.UpdateWorkflowPipelineParams{
		ID:           workflowID,
		PipelineYaml: rawYAML,
		PipelineDef:  mustJSON(p),
		DagWaves:     mustJSON(dagWaveNames),
	})
	if err != nil {
		return "", fmt.Errorf("engine: update workflow: %w", err)
	}

	// Create step rows.
	for waveIdx, wave := range waves {
		for _, step := range wave {
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

			err = qtx.InsertStep(ctx, db.InsertStepParams{
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
			})
			if err != nil {
				return "", fmt.Errorf("engine: insert step %s: %w", step.Name, err)
			}
		}
	}

	// Advance workflow — queues wave-0 steps.
	if err := advanceWorkflow(ctx, qtx, workflowID, 0); err != nil {
		return "", fmt.Errorf("engine: advance: %w", err)
	}

	// Link pipeline_runs to workflow — verify it actually updates.
	wfID := workflowID
	ref := input.Ref
	repo := input.Repo
	rowsAffected, err := qtx.UpdateRunWorkflow(ctx, db.UpdateRunWorkflowParams{
		ID:         input.RunID,
		WorkflowID: &wfID,
		Branch:     &ref,
		Repo:       &repo,
	})
	if err != nil {
		return "", fmt.Errorf("engine: link pipeline_run: %w", err)
	}
	if rowsAffected == 0 {
		return "", fmt.Errorf("engine: pipeline_run %s not updated", input.RunID)
	}

	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("engine: commit: %w", err)
	}

	_ = db.New(e.pool).NotifyEngine(ctx, workflowID)

	logger.Info().Str("workflowID", workflowID).Int("steps", len(p.Steps)).Msg("engine: workflow started")

	return workflowID, nil
}

// CompleteStep reports that a step has finished.
// Idempotent: calling twice with the same token for a terminal step returns nil.
func (e *PgEngine) CompleteStep(ctx context.Context, encodedToken string, result StepResult) error {
	token, err := DecodeTaskToken(encodedToken, e.signingKey)
	if err != nil {
		return err
	}

	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("engine: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
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
	stepID := stepRow.ID
	currentStatus := stepRow.Status
	maxAttempts := int(stepRow.MaxAttempts)
	retryBackoff := stepRow.RetryBackoff
	retryIntervalSec := int(stepRow.RetryIntervalSeconds)

	// Idempotency: already terminal → success.
	if isTerminal(currentStatus) {
		return nil
	}

	newStatus := "succeeded"
	if !result.Success {
		newStatus = "failed"
	}
	observe.StepsCompleted.Add(ctx, 1, metric.WithAttributes(attribute.String("status", newStatus)))

	// Update step.
	err = qtx.UpdateStepResult(ctx, db.UpdateStepResultParams{
		ID:     stepID,
		Status: newStatus,
		Result: mustJSON(result),
	})
	if err != nil {
		return fmt.Errorf("engine: update step: %w", err)
	}

	// Cancel timeout timer.
	if err := qtx.CancelTimer(ctx, db.CancelTimerParams{
		WorkflowID: token.WorkflowID,
		StepName:   token.StepName,
		TimerType:  "timeout",
	}); err != nil {
		log.Warn().Err(err).Str("step", token.StepName).Msg("engine: failed to cancel timeout timer")
	}

	// Update workflow step_outputs.
	outputJSON := mustJSON(map[string]StepResult{token.StepName: result})
	err = qtx.UpdateStepOutputs(ctx, db.UpdateStepOutputsParams{
		ID:      token.WorkflowID,
		Column2: outputJSON,
	})
	if err != nil {
		return fmt.Errorf("engine: update step_outputs: %w", err)
	}

	// Handle retry if needed.
	if !result.Success && token.Attempt+1 < maxAttempts {
		newAttempt := token.Attempt + 1
		err = qtx.CreateRetryStep(ctx, db.CreateRetryStepParams{
			NewAttempt:   int32(newAttempt),
			SourceStepID: stepID,
		})
		if err != nil {
			return fmt.Errorf("engine: create retry step: %w", err)
		}

		backoff := backoffDuration(token.Attempt, retryBackoff, retryIntervalSec)
		err = qtx.UpsertTimer(ctx, db.UpsertTimerParams{
			WorkflowID: token.WorkflowID,
			StepName:   token.StepName,
			TimerType:  "retry_backoff",
			Secs:       backoff.Seconds(),
		})
		if err != nil {
			return fmt.Errorf("engine: create retry timer: %w", err)
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
	return nil
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
func (e *PgEngine) CancelWorkflow(ctx context.Context, workflowID string) error {
	tx, err := e.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := db.New(e.pool).WithTx(tx)

	if err := qtx.CancelWorkflow(ctx, workflowID); err != nil {
		return fmt.Errorf("engine: cancel workflow: %w", err)
	}
	if err := qtx.CancelPendingSteps(ctx, workflowID); err != nil {
		log.Error().Err(err).Str("workflowID", workflowID).Msg("engine: failed to cancel pending steps")
	}
	if err := qtx.CancelAllWorkflowTimers(ctx, workflowID); err != nil {
		log.Error().Err(err).Str("workflowID", workflowID).Msg("engine: failed to cancel timers")
	}
	if err := qtx.CancelChildWorkflows(ctx, &workflowID); err != nil {
		log.Error().Err(err).Str("workflowID", workflowID).Msg("engine: failed to cancel child workflows")
	}

	return tx.Commit(ctx)
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

func isTerminal(status string) bool {
	return status == "succeeded" || status == "failed" || status == "skipped" || status == "cancelled"
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
