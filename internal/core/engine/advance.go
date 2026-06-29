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

	// 7. Per-edge DAG gating. Walk steps in topological order (dag_waves provides a
	// valid ordering — every dependency appears in an earlier wave) and queue each
	// pending step as soon as ITS OWN dependencies are terminal, rather than waiting
	// for the entire previous wave to finish. Independent branches advance
	// independently; a fast branch no longer blocks on a slow unrelated sibling.
	// We continue past failed steps so `when: onFailure`/`always` steps still run.
	//
	// `when:` is evaluated against the step's OWN upstream subgraph (dependency-
	// scoped), not the global pipeline state: an onSuccess step is skipped only if a
	// transitive ancestor of THAT step failed, so a failure in an unrelated branch
	// leaves this step alone. This matches GitHub Actions success()/failure() and
	// Argo's depends. The global pipeline-failure verdict still drives the final
	// workflow status (step 8), not per-step gating.
	for _, wave := range dagWaves {
		for _, stepName := range wave {
			step, exists := stepByName[stepName]
			if !exists || step.status != stepPending {
				continue
			}

			// Eligible only once every upstream dependency is terminal.
			if !dependenciesTerminal(step.dependsOn, stepByName) {
				continue
			}

			// Dependency-scoped `when:` — did any of THIS step's ancestors fail?
			if !stepShouldRun(step.when, ancestorFailed(stepName, stepByName)) {
				stepByName[stepName] = markStep(ctx, qtx, workflowID, step, stepSkipped,
					withReason("when condition not met"))
				continue
			}

			// Evaluate if-condition.
			if step.ifCondition != "" {
				exprCtx := buildEngineExprContext(input, stepOutputs)
				shouldRun, _ := pipeline.EvalCondition(step.ifCondition, exprCtx)
				if !shouldRun {
					stepByName[stepName] = markStep(ctx, qtx, workflowID, step, stepSkipped,
						withReason("if condition evaluated false"))
					continue
				}
			}

			stepByName[stepName] = markStep(ctx, qtx, workflowID, step, stepQueued)
		}
	}

	// 8. Check if all waves are complete and finish the workflow. The FINAL status is
	// still a global verdict: any unrecoverable step failure fails the run, even if
	// dependency-scoped `when:` let independent branches finish.
	if allWavesComplete(dagWaves, stepByName) {
		finalStatus := "succeeded"
		if isPipelineFailed(dagWaves, stepByName) {
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
			} else {
				emitStepEvent(ctx, qtx, stepTransition{
					workflowID: *parent.ParentID, stepName: *parent.ParentStep, attempt: -1,
					from: stepRunning, to: finalStatus, eventType: "invoke_completed", actor: actorEngine,
					metadata: map[string]any{"childWorkflow": workflowID},
				})
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

// stepShouldRun reports whether a step should be queued given whether its upstream
// (its transitive dependency subgraph) has an unrecoverable failure. Dependency-
// scoped: upstreamFailed is computed per step by ancestorFailed, not globally.
//
//   - "" or "onSuccess": run only if no ancestor failed
//   - "onFailure":       run only if an ancestor failed
//   - "always":          run unconditionally
func stepShouldRun(when string, upstreamFailed bool) bool {
	switch when {
	case "onFailure":
		return upstreamFailed
	case "always":
		return true
	default: // "" or "onSuccess"
		return !upstreamFailed
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
	name        string
	status      string
	attempt     int
	onFailure   string
	ifCondition string
	when        string
	dependsOn   []string
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
			name:      row.Name,
			status:    row.Status,
			attempt:   int(row.Attempt),
			onFailure: row.OnFailure,
			dependsOn: decodeDependsOn(row.DependsOn),
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

// decodeDependsOn normalizes the step_def->'dependsOn' JSONB (delivered by pgx as
// interface{}, typically []interface{}) into a []string of upstream step names.
func decodeDependsOn(raw interface{}) []string {
	if raw == nil {
		return nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var deps []string
	if json.Unmarshal(b, &deps) != nil {
		return nil
	}
	return deps
}

// markStep applies a step status change through the transition chokepoint (which
// validates, updates the row, and records a history event) and returns the
// updated stepRow with all other fields preserved, ready to store back into the
// in-memory stepByName map.
func markStep(ctx context.Context, qtx *db.Queries, workflowID string, s stepRow, to string, opts ...func(*stepTransition)) stepRow {
	t := stepTransition{
		stepID:     s.id,
		workflowID: workflowID,
		stepName:   s.name,
		attempt:    s.attempt,
		from:       s.status,
		to:         to,
		actor:      actorEngine,
	}
	for _, o := range opts {
		o(&t)
	}
	if err := transitionStep(ctx, qtx, t); err != nil {
		log.Error().Err(err).Str("step", s.name).Str("to", to).
			Msg("engine: step transition failed")
		return s
	}
	s.status = to
	return s
}

// markStep option helpers.
func withReason(reason string) func(*stepTransition) {
	return func(t *stepTransition) { t.reason = reason }
}

func withResult(r StepResult) func(*stepTransition) {
	return func(t *stepTransition) { t.result = &r }
}

func withActor(actor string) func(*stepTransition) {
	return func(t *stepTransition) { t.actor = actor }
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

		newStatus := stepSucceeded
		if !signal.Success {
			newStatus = stepFailed
		}
		result := StepResult{StepName: signal.StepName, Success: signal.Success, Error: signal.Reason}
		stepByName[signal.StepName] = markStep(ctx, qtx, workflowID, step, newStatus,
			withResult(result), withActor(actorInformer), withReason(signal.Reason))

		log.Info().
			Str("step", signal.StepName).
			Bool("success", signal.Success).
			Msg("engine: consumed step-result signal")
	}
	return nil
}

// ─────────────────────────────────────────────────────────────
// dependency / wave helpers
// ─────────────────────────────────────────────────────────────

// dependenciesTerminal reports whether every upstream dependency of a step has
// reached a terminal status, making the step eligible to leave 'pending'. A
// dependency name not present in the step map is treated as satisfied: the DAG
// resolver prunes edges to env-filtered steps, and a non-existent dependency
// cannot gate progress (matching the prior wave model, which also ignored absent
// names).
func dependenciesTerminal(deps []string, stepByName map[string]stepRow) bool {
	for _, d := range deps {
		s, ok := stepByName[d]
		if !ok {
			continue
		}
		if !isTerminal(s.status) {
			return false
		}
	}
	return true
}

// ancestorFailed reports whether any TRANSITIVE dependency of stepName failed
// unrecoverably (status 'failed' with onFailure != "continue"). This scopes the
// `when:` condition to the step's own upstream subgraph rather than the whole
// pipeline, so an onSuccess step in one branch is unaffected by a failure in an
// unrelated branch — dependency-scoped semantics matching GitHub Actions
// success()/failure() and Argo's depends. Walking the full ancestor closure (not
// just direct deps) means a chain/diamond failure still propagates even when the
// direct dependency was skipped because of it. The DAG is acyclic; the seen set
// guards against a malformed cycle and bounds the walk.
func ancestorFailed(stepName string, stepByName map[string]stepRow) bool {
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(name string) bool {
		s, ok := stepByName[name]
		if !ok {
			return false
		}
		for _, dep := range s.dependsOn {
			if seen[dep] {
				continue
			}
			seen[dep] = true
			d, ok := stepByName[dep]
			if !ok {
				continue
			}
			if d.status == stepFailed && d.onFailure != "continue" {
				return true
			}
			if walk(dep) {
				return true
			}
		}
		return false
	}
	return walk(stepName)
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

// ─────────────────────────────────────────────────────────────
// expression context
// ─────────────────────────────────────────────────────────────

func buildEngineExprContext(input StartWorkflowInput, stepOutputs map[string]StepResult) pipeline.ExprContext {
	ctx := pipeline.ExprContext{
		// git/run/inputs come from the generic Inputs bag (populated by
		// normalizeInputs for CI runs), not from typed fields — the engine is
		// product-neutral here. `inputs` is the manual-trigger / workflow input
		// namespace; it degrades to empty when a product hasn't populated it (it
		// was previously aliased to env by mistake — env and inputs are distinct).
		"git":    exprNamespace(input.Inputs, "git"),
		"run":    exprNamespace(input.Inputs, "run"),
		"env":    input.Env,
		"inputs": exprNamespace(input.Inputs, "inputs"),
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
