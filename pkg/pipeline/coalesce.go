package pipeline

// CoalesceSteps rewrites a pipeline's top-level step list so that consecutive
// "run" steps that share the pipeline-level runner are merged into a single
// nested steps: group. The merged group runs as one K8s Job with a shared
// emptyDir workspace, eliminating cross-step workspace sync for the common
// case of simple sequential pipelines.
//
// Coalescing is only applied when:
//  1. The pipeline has a top-level runner: field set.
//  2. No step in the pipeline uses an explicit dependsOn — pipelines that
//     express their own parallelism are returned unchanged.
//
// A step is coalesceable if:
//   - Its ExecType is "run" (gates, nested steps, and template steps are left alone)
//   - It has no per-step runner override (or its runner matches the pipeline runner)
//
// A coalescing run of 1 step is left as-is. A run of 2+ becomes a wrapper
// steps: step whose name is taken from the first step in the group.
//
// CoalesceSteps returns the original Pipeline pointer when no changes are
// needed, or a shallow copy with a new Steps slice when changes are made.
func CoalesceSteps(p *Pipeline) *Pipeline {
	if p.Runner == "" {
		return p
	}

	// If any step declares explicit dependencies, the pipeline already expresses
	// its intended parallelism — leave it alone.
	for _, s := range p.Steps {
		if len(s.DependsOn) > 0 {
			return p
		}
	}

	coalesced := coalesceSteps(p.Steps, p.Runner)
	if stepsEqual(p.Steps, coalesced) {
		return p
	}

	out := *p // shallow copy
	out.Steps = coalesced
	return &out
}

// coalesceSteps performs the actual grouping on a step slice.
func coalesceSteps(steps []Step, pipelineRunner string) []Step {
	var result []Step
	i := 0
	for i < len(steps) {
		if !isCoalesceable(steps[i], pipelineRunner) {
			result = append(result, steps[i])
			i++
			continue
		}

		// Collect the maximal run of consecutive coalesceable steps.
		j := i + 1
		for j < len(steps) && isCoalesceable(steps[j], pipelineRunner) {
			j++
		}

		group := steps[i:j]
		if len(group) == 1 {
			// Not worth wrapping a single step — pass through as-is.
			result = append(result, group[0])
		} else {
			result = append(result, wrapGroup(group, pipelineRunner))
		}
		i = j
	}
	return result
}

// isCoalesceable reports whether step s can be merged into a coalesced group
// for the given pipeline-level runner.
func isCoalesceable(s Step, pipelineRunner string) bool {
	if s.ExecType() != "run" {
		return false
	}
	// No per-step runner override, or the override matches the pipeline runner.
	if s.Runner != "" && s.Runner != pipelineRunner {
		return false
	}
	// Steps with non-default `when:` semantics (onFailure, always) must remain
	// as independent steps so the engine can evaluate the `when:` condition per
	// step. Coalescing them into a shared pod would lose that granularity.
	if s.When != "" && s.When != "onSuccess" {
		return false
	}
	return true
}

// wrapGroup creates a synthetic steps: wrapper step from a group of run steps.
// The wrapper takes the name of the first step. The nested steps have their
// per-step runner cleared (they inherit the wrapper's runner) and are otherwise
// preserved verbatim.
func wrapGroup(group []Step, runner string) Step {
	nested := make([]Step, len(group))
	for i, s := range group {
		s.Runner = "" // inherited from wrapper
		nested[i] = s
	}
	return Step{
		Name:   group[0].Name,
		Runner: runner,
		Steps:  nested,
	}
}

// stepsEqual reports whether two step slices are identical by name/count
// (cheap proxy for "no changes were made").
func stepsEqual(a, b []Step) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].ExecType() != b[i].ExecType() {
			return false
		}
	}
	return true
}
