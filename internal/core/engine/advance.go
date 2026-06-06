package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/rs/zerolog/log"
)

// maxAdvanceDepth prevents infinite recursion in invoke chains.
const maxAdvanceDepth = 10

// advanceWorkflow is the heart of the engine. Runs inside a Postgres transaction
// and progresses the workflow as far as possible in a single pass.
//
// depth tracks recursion for child workflow completion → parent advancement.
// Idempotent — safe to call multiple times with same state.
func advanceWorkflow(ctx context.Context, qtx *db.Queries, workflowID string, depth int) error {
	if depth > maxAdvanceDepth {
		return fmt.Errorf("engine: max advancement depth exceeded (possible invoke cycle)")
	}

	// 1. Lock the workflow row (serializes per-workflow advancement).
	wf, err := qtx.LockWorkflow(ctx, workflowID)
	if err != nil {
		return fmt.Errorf("engine: lock workflow: %w", err)
	}
	if wf.Status != "running" {
		return nil
	}

	// 2. Parse cached DAG waves.
	var dagWaves [][]string
	if err := json.Unmarshal(wf.DagWaves, &dagWaves); err != nil {
		return fmt.Errorf("engine: unmarshal dag_waves: %w", err)
	}

	// 3. Load latest attempt for each step.
	stepByName, err := loadLatestSteps(ctx, qtx, workflowID)
	if err != nil {
		return fmt.Errorf("engine: load steps: %w", err)
	}

	// 4. Parse step outputs and input.
	var stepOutputs map[string]StepResult
	_ = json.Unmarshal(wf.StepOutputs, &stepOutputs)
	if stepOutputs == nil {
		stepOutputs = make(map[string]StepResult)
	}
	var input StartWorkflowInput
	_ = json.Unmarshal(wf.Input, &input)

	// 5. Consume pending step-result signals (informer safety net).
	if err := consumeStepSignals(ctx, qtx, workflowID, stepByName); err != nil {
		log.Warn().Err(err).Str("workflow", workflowID).Msg("engine: signal consumption error")
	}

	// 6. Determine whether the pipeline has experienced an unrecoverable failure
	// so far. This drives `when:` semantics for pending steps.
	//
	// A step failure is unrecoverable when its onFailure policy is not "continue".
	// We compute this from already-terminal steps before processing any pending
	// ones, so that `when: onFailure` steps in a later wave see the correct state.
	pipelineFailed := isPipelineFailed(dagWaves, stepByName)

	// 7. Walk waves in order. Unlike the old early-exit model, we continue past
	// failed waves so that steps with `when: onFailure` or `when: always` can
	// still be queued. Steps whose `when:` condition is not met are skipped.
	for waveIdx, wave := range dagWaves {
		allComplete := true

		for _, stepName := range wave {
			step, exists := stepByName[stepName]
			if !exists {
				continue
			}

			switch step.status {
			case "succeeded", "skipped", "failed", "cancelled":
				// Already terminal — nothing to do.

			case "pending":
				// Wait for the previous wave to finish before queuing this one.
				if waveIdx > 0 && !waveComplete(dagWaves[waveIdx-1], stepByName) {
					allComplete = false
					continue
				}

				// Check `when:` condition against current pipeline failure state.
				if !stepShouldRun(step.when, pipelineFailed) {
					setStepStatus(ctx, qtx, step.id, "skipped")
					stepByName[stepName] = stepRow{
						id:          step.id,
						status:      "skipped",
						onFailure:   step.onFailure,
						ifCondition: step.ifCondition,
						when:        step.when,
					}
					continue
				}

				// Evaluate if-condition.
				if step.ifCondition != "" {
					exprCtx := buildEngineExprContext(input, stepOutputs)
					shouldRun, _ := pipeline.EvalCondition(step.ifCondition, exprCtx)
					if !shouldRun {
						setStepStatus(ctx, qtx, step.id, "skipped")
						stepByName[stepName] = stepRow{
							id:          step.id,
							status:      "skipped",
							onFailure:   step.onFailure,
							ifCondition: step.ifCondition,
							when:        step.when,
						}
						continue
					}
				}

				setStepStatus(ctx, qtx, step.id, "queued")
				stepByName[stepName] = stepRow{
					id:          step.id,
					status:      "queued",
					onFailure:   step.onFailure,
					ifCondition: step.ifCondition,
					when:        step.when,
				}
				allComplete = false

			default:
				// Running, queued, waiting — not yet done.
				allComplete = false
			}
		}

		if !allComplete {
			break
		}
	}

	// 8. Check if all waves are complete and finish the workflow.
	if allWavesComplete(dagWaves, stepByName) {
		// Re-evaluate pipelineFailed with the now-complete step map.
		pipelineFailed = isPipelineFailed(dagWaves, stepByName)
		finalStatus := "succeeded"
		if pipelineFailed {
			finalStatus = "failed"
		}
		finishWorkflow(ctx, qtx, workflowID, finalStatus)

		// If child workflow, complete the parent's invoke step with child's output.
		parent, _ := qtx.GetWorkflowParent(ctx, workflowID)

		if parent.ParentID != nil && parent.ParentStep != nil {
			// Propagate child outputs to parent's invoke step result.
			childResult := StepResult{
				StepName: *parent.ParentStep,
				Success:  finalStatus == "succeeded",
			}
			if finalStatus != "succeeded" {
				childResult.Error = "child workflow " + finalStatus
			}
			// Copy child step_outputs into the result's Outputs map.
			childResult.Outputs = make(map[string]string)
			for name, sr := range stepOutputs {
				for k, v := range sr.Outputs {
					childResult.Outputs[name+"."+k] = v
				}
			}

			resultJSON := mustJSON(childResult)
			if err := qtx.CompleteParentInvokeStep(ctx, db.CompleteParentInvokeStepParams{
				NewStatus:  finalStatus,
				Result:     resultJSON,
				WorkflowID: *parent.ParentID,
				StepName:   *parent.ParentStep,
			}); err != nil {
				log.Error().Err(err).Str("parent", *parent.ParentID).Msg("engine: failed to complete parent invoke step")
			}
			if err := qtx.UpdateStepOutputs(ctx, db.UpdateStepOutputsParams{
				ID:      *parent.ParentID,
				Column2: mustJSON(map[string]StepResult{*parent.ParentStep: childResult}),
			}); err != nil {
				log.Error().Err(err).Str("parent", *parent.ParentID).Msg("engine: failed to update parent step outputs")
			}
			// Advance the parent (with incremented depth to prevent cycles).
			if err := advanceWorkflow(ctx, qtx, *parent.ParentID, depth+1); err != nil {
				log.Error().Err(err).Str("parent", *parent.ParentID).Msg("engine: failed to advance parent")
			}
		}
	}

	return nil
}

// stepShouldRun reports whether a step should be queued given the current
// pipeline failure state.
//
//   - "" or "onSuccess": run only if the pipeline has not failed
//   - "onFailure":       run only if the pipeline has failed
//   - "always":          run unconditionally
func stepShouldRun(when string, pipelineFailed bool) bool {
	switch when {
	case "onFailure":
		return pipelineFailed
	case "always":
		return true
	default: // "" or "onSuccess"
		return !pipelineFailed
	}
}

// isPipelineFailed reports whether any already-terminal step failed in a way
// that is not recovered by a continueOnError policy.
func isPipelineFailed(dagWaves [][]string, stepByName map[string]stepRow) bool {
	for _, wave := range dagWaves {
		for _, name := range wave {
			s, ok := stepByName[name]
			if ok && s.status == "failed" && s.onFailure != "continue" {
				return true
			}
		}
	}
	return false
}

// ─────────────────────────────────────────────────────────────
// step loading
// ─────────────────────────────────────────────────────────────

type stepRow struct {
	id          string
	status      string
	onFailure   string
	ifCondition string
	when        string
}

func loadLatestSteps(ctx context.Context, qtx *db.Queries, workflowID string) (map[string]stepRow, error) {
	rows, err := qtx.LatestStepsByWorkflow(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	result := make(map[string]stepRow)
	for _, row := range rows {
		s := stepRow{
			id:        row.ID,
			status:    row.Status,
			onFailure: row.OnFailure,
		}
		if row.IfCondition != nil {
			if v, ok := row.IfCondition.(string); ok {
				s.ifCondition = v
			}
		}
		if row.WhenCondition != nil {
			if v, ok := row.WhenCondition.(string); ok {
				s.when = v
			}
		}
		result[row.Name] = s
	}
	return result, nil
}

func setStepStatus(ctx context.Context, qtx *db.Queries, stepID, status string) {
	var err error
	switch status {
	case "queued":
		err = qtx.SetStepQueued(ctx, stepID)
	case "skipped":
		err = qtx.SetStepSkipped(ctx, stepID)
	default:
		err = qtx.UpdateStepResult(ctx, db.UpdateStepResultParams{
			ID:     stepID,
			Status: status,
			Result: nil,
		})
	}
	if err != nil {
		log.Error().Err(err).Str("stepID", stepID).Str("status", status).
			Msg("engine: failed to set step status")
	}
}

// ─────────────────────────────────────────────────────────────
// signal handling
// ─────────────────────────────────────────────────────────────

func consumeStepSignals(ctx context.Context, qtx *db.Queries, workflowID string, stepByName map[string]stepRow) error {
	payloads, err := qtx.ConsumeStepResultSignals(ctx, workflowID)
	if err != nil {
		return err
	}
	for _, payloadJSON := range payloads {
		var signal struct {
			StepName string `json:"stepName"`
			Success  bool   `json:"success"`
			Reason   string `json:"reason"`
		}
		if json.Unmarshal(payloadJSON, &signal) != nil {
			continue
		}

		step, exists := stepByName[signal.StepName]
		if !exists || isTerminal(step.status) {
			continue
		}

		newStatus := "succeeded"
		if !signal.Success {
			newStatus = "failed"
		}
		setStepStatus(ctx, qtx, step.id, newStatus)
		stepByName[signal.StepName] = stepRow{
			id:          step.id,
			status:      newStatus,
			onFailure:   step.onFailure,
			ifCondition: step.ifCondition,
			when:        step.when,
		}

		log.Info().
			Str("step", signal.StepName).
			Bool("success", signal.Success).
			Msg("engine: consumed step-result signal")
	}
	return nil
}

// ─────────────────────────────────────────────────────────────
// wave helpers
// ─────────────────────────────────────────────────────────────

func waveComplete(waveNames []string, stepByName map[string]stepRow) bool {
	for _, name := range waveNames {
		s, ok := stepByName[name]
		if !ok || !isTerminal(s.status) {
			return false
		}
	}
	return true
}

func allWavesComplete(waves [][]string, stepByName map[string]stepRow) bool {
	for _, wave := range waves {
		if !waveComplete(wave, stepByName) {
			return false
		}
	}
	return true
}

// ─────────────────────────────────────────────────────────────
// expression context
// ─────────────────────────────────────────────────────────────

func buildEngineExprContext(input StartWorkflowInput, stepOutputs map[string]StepResult) pipeline.ExprContext {
	ctx := pipeline.ExprContext{
		// git/run come from the generic Inputs bag (populated by normalizeInputs
		// for CI runs), not from typed fields — the engine is product-neutral here.
		"git":    exprNamespace(input.Inputs, "git"),
		"run":    exprNamespace(input.Inputs, "run"),
		"env":    input.Env,
		"inputs": input.Env,
	}

	steps := make(map[string]any, len(stepOutputs))
	for name, result := range stepOutputs {
		stepCtx := map[string]any{"status": "success"}
		if !result.Success {
			stepCtx["status"] = "failure"
		}
		for k, v := range result.Outputs {
			stepCtx[k] = v
		}
		steps[name] = stepCtx
	}
	ctx["steps"] = steps

	return ctx
}

// exprNamespace returns the named namespace (e.g. "git", "run") from the generic
// Inputs bag as a map for expression evaluation. After JSON round-tripping the
// persisted input, nested objects are map[string]interface{}; an absent or
// wrongly-typed namespace yields an empty map so expressions degrade to empty
// values rather than erroring.
func exprNamespace(inputs map[string]any, key string) map[string]any {
	if inputs != nil {
		if ns, ok := inputs[key].(map[string]any); ok {
			return ns
		}
	}
	return map[string]any{}
}
