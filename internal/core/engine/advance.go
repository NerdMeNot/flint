package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/pkg/pipeline"
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
	// failedAncestor[name] answers "did anything in this step's transitive
	// upstream fail unrecoverably". It is folded forward as we walk rather than
	// recomputed per step: dag_waves is a topological order, so by the time a
	// step is reached every one of its dependencies has already been assigned a
	// verdict, and the step's own verdict is its direct dependencies' verdicts
	// OR'd with their statuses. That makes the whole pass O(V+E); the previous
	// per-step DFS over the full ancestor closure made it O(V·(V+E)), which is
	// the term that bites once matrix expansion makes V large.
	//
	// Folding forward (rather than precomputing) is what keeps it correct while
	// the pass mutates state: a step marked skipped or failed here is already
	// written back into stepByName before any downstream step reads it.
	failedAncestor := make(map[string]bool, len(stepByName))
	folded := make(map[string]bool, len(stepByName))

	for _, wave := range dagWaves {
		for _, stepName := range wave {
			step, exists := stepByName[stepName]
			if !exists {
				continue
			}

			// Computed for EVERY step, not just pending ones — a terminal step is
			// still an ancestor of later steps and has to carry a verdict.
			upstreamFailed := false
			for _, dep := range step.dependsOn {
				d, ok := stepByName[dep]
				if !ok {
					continue // pruned by env filtering; cannot gate progress
				}
				if !folded[dep] {
					// This edge points at a step dag_waves has not placed earlier,
					// so the fold has no verdict for it yet. Rather than silently
					// under-report a failure, fall back to the full walk for this
					// step. Unreachable while waves are topological; here so the
					// optimisation cannot become a correctness regression if that
					// ever stops holding.
					upstreamFailed = ancestorFailed(stepName, stepByName)
					break
				}
				if failedAncestor[dep] || (d.status == stepFailed && d.onFailure != "continue") {
					upstreamFailed = true
					break
				}
			}
			failedAncestor[stepName] = upstreamFailed
			folded[stepName] = true

			if step.status != stepPending {
				continue
			}

			// Eligible only once every upstream dependency is terminal.
			if !dependenciesTerminal(step.dependsOn, stepByName) {
				continue
			}
			if !stepShouldRun(step.when, upstreamFailed) {
				stepByName[stepName] = markStep(ctx, qtx, workflowID, step, stepSkipped,
					withReason("when condition not met"))
				continue
			}

			// Evaluate if-condition. An evaluation ERROR fails the step loudly
			// — the worst failure mode is an expression that lints clean and
			// then silently skips the step at runtime. Deciding "don't run"
			// must be reserved for expressions that evaluated to false.
			if step.ifCondition != "" {
				exprCtx := buildEngineExprContext(input, stepOutputs, step.dependsOn, upstreamFailed)
				shouldRun, evalErr := pipeline.EvalCondition(step.ifCondition, exprCtx)
				if evalErr != nil {
					reason := fmt.Sprintf("if condition %q failed to evaluate: %v", step.ifCondition, evalErr)
					result := StepResult{StepName: stepName, Success: false, Error: reason}
					stepByName[stepName] = markStep(ctx, qtx, workflowID, step, stepFailed,
						withReason(reason), withResult(result))
					continue
				}
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
	id               string
	name             string
	status           string
	attempt          int
	onFailure        string
	ifCondition      string
	when             string
	dependsOn        []string
	maxAttempts      int
	retryBackoff     string
	retryIntervalSec int
}

func loadLatestSteps(ctx context.Context, qtx *db.Queries, workflowID string) (map[string]stepRow, error) {
	rows, err := qtx.LatestStepsByWorkflow(ctx, workflowID)
	if err != nil {
		return nil, err
	}
	result := make(map[string]stepRow)
	for _, row := range rows {
		s := stepRow{
			id:               row.ID,
			name:             row.Name,
			status:           row.Status,
			attempt:          int(row.Attempt),
			onFailure:        row.OnFailure,
			dependsOn:        decodeDependsOn(row.DependsOn),
			maxAttempts:      int(row.MaxAttempts),
			retryBackoff:     row.RetryBackoff,
			retryIntervalSec: int(row.RetryIntervalSeconds),
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
		if err := json.Unmarshal(payloadJSON, &signal); err != nil {
			// Never drop silently — a malformed signal here means a crashed
			// step would stay running until the timeout sweep.
			log.Warn().Err(err).Str("workflow", workflowID).
				Str("payload", string(payloadJSON)).
				Msg("engine: unparseable step-result signal dropped")
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
		updated := markStep(ctx, qtx, workflowID, step, newStatus,
			withResult(result), withActor(actorInformer), withReason(signal.Reason))
		stepByName[signal.StepName] = updated

		// A machine-lost / infra failure must honour the step's retry policy the
		// same way an agent-reported failure (CompleteStep) and a dispatch failure
		// (failStepAndAdvance) do — otherwise a spot reclaim fails a step
		// permanently even with attempts remaining. Schedule the next attempt and,
		// if one was parked, reflect it in the working set so THIS advance pass
		// treats the step as non-terminal (blocks downstream) instead of
		// propagating a terminal failure.
		if !signal.Success {
			scheduled, err := maybeScheduleRetry(ctx, qtx, step.id, workflowID, signal.StepName,
				step.attempt, step.maxAttempts, step.retryBackoff, step.retryIntervalSec)
			if err != nil {
				return err
			}
			if scheduled {
				updated.status = stepRetryWait
				updated.attempt = step.attempt + 1
				stepByName[signal.StepName] = updated
			}
		}

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

func buildEngineExprContext(input StartWorkflowInput, stepOutputs map[string]StepResult, dependsOn []string, upstreamFailed bool) pipeline.ExprContext {
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
		steps[name] = stepResultCtx(result)
	}
	ctx["steps"] = steps

	// needs.<job> — scoped to THIS step's direct dependencies (the spec's
	// "direct-only output scope"): needs.build.outputs.version and
	// needs.build.result. Matrix-expanded dependency names (build::1.26) are
	// exposed under their base job name.
	needs := make(map[string]any, len(dependsOn))
	for _, dep := range dependsOn {
		if result, ok := stepOutputs[dep]; ok {
			needs[baseStepName(dep)] = stepResultCtx(result)
		}
	}
	ctx["needs"] = needs

	// Status functions for if: conditions, scoped to THIS step's dependency
	// subgraph (the same scope as `when:` — upstreamFailed is ancestorFailed for
	// this step, not a global pipeline verdict). They let an expression react to
	// upstream outcome: `if: ${{ failure() }}`, `if: ${{ success() && branch ==
	// 'main' }}`. Note these only matter once the step is reached: a job keeps
	// the default onSuccess `when`, so to run on failure it must also set `when:
	// onFailure`/`always` (which is what lets the engine evaluate if: at all when
	// an ancestor failed). At validation time upstreamFailed is false — only the
	// functions' presence matters for the type-check.
	ctx["success"] = func() bool { return !upstreamFailed }
	ctx["failure"] = func() bool { return upstreamFailed }
	ctx["always"] = func() bool { return true }

	// Context-independent helpers (fromJSON/toJSON/format), shared verbatim with
	// the validation context so an if: using them can't validate-clean then break.
	for k, fn := range pipeline.StdExprFuncs() {
		ctx[k] = fn
	}

	return ctx
}

// stepResultCtx renders a step result as its expression namespace:
// {result: "success"|"failure", outputs: {...}, plus flattened output keys
// for the legacy steps.<name>.<key> form}.
func stepResultCtx(result StepResult) map[string]any {
	status := "success"
	if !result.Success {
		status = "failure"
	}
	outputs := map[string]any{}
	stepCtx := map[string]any{"status": status, "result": status, "outputs": outputs}
	for k, v := range result.Outputs {
		outputs[k] = v
		if _, reserved := stepCtx[k]; !reserved {
			stepCtx[k] = v
		}
	}
	return stepCtx
}

// baseStepName strips a matrix-variant suffix: "build::1.26" → "build".
func baseStepName(name string) string {
	if i := strings.Index(name, "::"); i >= 0 {
		return name[:i]
	}
	return name
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

// ValidationExprContext returns the EXACT expression-context shape the engine
// provides to if: conditions at runtime, with zero values, for compile-time
// expression checking by product validators and the CLI. It is built by the
// same code path as the runtime context (normalizeInputs +
// buildEngineExprContext), so validation and runtime can never drift apart —
// an expression that type-checks here evaluates at runtime, and vice versa.
func ValidationExprContext() pipeline.ExprContext {
	in := StartWorkflowInput{Kind: "ci", Env: map[string]string{}}
	in.normalizeInputs()
	return buildEngineExprContext(in, nil, nil, false)
}
