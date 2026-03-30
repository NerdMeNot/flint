package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/NerdMeNot/flint/internal/db"
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

	// 6. Walk waves in order.
	for waveIdx, wave := range dagWaves {
		allComplete := true
		waveFailed := false

		for _, stepName := range wave {
			step, exists := stepByName[stepName]
			if !exists {
				continue
			}

			switch step.status {
			case "succeeded", "skipped":
				// Done.
			case "failed":
				if step.onFailure != "continue" {
					waveFailed = true
				}
			case "cancelled":
				// Ignore.
			case "pending":
				if waveIdx > 0 && !waveComplete(dagWaves[waveIdx-1], stepByName) {
					allComplete = false
					continue
				}
				// Evaluate if-condition.
				if step.ifCondition != "" {
					exprCtx := buildEngineExprContext(input, stepOutputs)
					shouldRun, _ := pipeline.EvalCondition(step.ifCondition, exprCtx)
					if !shouldRun {
						setStepStatus(ctx, qtx, step.id, "skipped")
						stepByName[stepName] = stepRow{
							id: step.id, status: "skipped",
							onFailure: step.onFailure, ifCondition: step.ifCondition,
						}
						continue
					}
				}
				setStepStatus(ctx, qtx, step.id, "queued")
				stepByName[stepName] = stepRow{
					id: step.id, status: "queued",
					onFailure: step.onFailure, ifCondition: step.ifCondition,
				}
				allComplete = false
			default:
				allComplete = false
			}
		}

		if waveFailed {
			finishWorkflow(ctx, qtx, workflowID, "failed")
			_ = qtx.CancelPendingSteps(ctx, workflowID)
			return nil
		}

		if !allComplete {
			break
		}
	}

	// 7. Check if all waves complete.
	if allWavesComplete(dagWaves, stepByName) {
		finalStatus := "succeeded"
		for _, wave := range dagWaves {
			for _, name := range wave {
				if s, ok := stepByName[name]; ok && s.status == "failed" {
					finalStatus = "failed"
				}
			}
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
			_ = qtx.CompleteParentInvokeStep(ctx, db.CompleteParentInvokeStepParams{
				NewStatus:  finalStatus,
				Result:     resultJSON,
				WorkflowID: *parent.ParentID,
				StepName:   *parent.ParentStep,
			})
			// Update parent's step_outputs.
			_ = qtx.UpdateStepOutputs(ctx, db.UpdateStepOutputsParams{
				ID:      *parent.ParentID,
				Column2: mustJSON(map[string]StepResult{*parent.ParentStep: childResult}),
			})
			// Advance the parent (with incremented depth to prevent cycles).
			if err := advanceWorkflow(ctx, qtx, *parent.ParentID, depth+1); err != nil {
				log.Error().Err(err).Str("parent", *parent.ParentID).Msg("engine: failed to advance parent")
			}
		}
	}

	return nil
}

type stepRow struct {
	id          string
	status      string
	onFailure   string
	ifCondition string
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
		// IfCondition is interface{} from sqlc (jsonb expression), convert to string
		if row.IfCondition != nil {
			if ifStr, ok := row.IfCondition.(string); ok {
				s.ifCondition = ifStr
			}
		}
		result[row.Name] = s
	}
	return result, nil
}

func setStepStatus(ctx context.Context, qtx *db.Queries, stepID, status string) {
	switch status {
	case "queued":
		_ = qtx.SetStepQueued(ctx, stepID)
	case "skipped":
		_ = qtx.SetStepSkipped(ctx, stepID)
	default:
		_ = qtx.UpdateStepResult(ctx, db.UpdateStepResultParams{
			ID:     stepID,
			Status: status,
			Result: nil,
		})
	}
}

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
			id: step.id, status: newStatus,
			onFailure: step.onFailure, ifCondition: step.ifCondition,
		}

		log.Info().
			Str("step", signal.StepName).
			Bool("success", signal.Success).
			Msg("engine: consumed step-result signal")
	}
	return nil
}

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

func buildEngineExprContext(input StartWorkflowInput, stepOutputs map[string]StepResult) pipeline.ExprContext {
	ctx := pipeline.ExprContext{
		"git": map[string]any{
			"sha": input.CommitSHA, "branch": input.Ref, "repoUrl": input.Repo,
		},
		"run": map[string]any{
			"id": input.RunID, "trigger": input.TriggerType,
		},
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
