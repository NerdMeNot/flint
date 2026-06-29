package ci

import (
	"testing"

	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/stretchr/testify/assert"
)

// diamond: a → {b, c} → d
func diamondWaves() [][]pipeline.Step {
	return [][]pipeline.Step{
		{{Name: "a"}},
		{{Name: "b", DependsOn: []string{"a"}}, {Name: "c", DependsOn: []string{"a"}}},
		{{Name: "d", DependsOn: []string{"b", "c"}}},
	}
}

func TestDownstreamClosure(t *testing.T) {
	waves := diamondWaves()

	// Trigger b → closure is {b, d} (d depends on b).
	cl := downstreamClosure(waves, map[string]bool{"b": true})
	assert.Equal(t, map[string]bool{"b": true, "d": true}, cl)

	// Trigger a → everything downstream.
	cl = downstreamClosure(waves, map[string]bool{"a": true})
	assert.Equal(t, map[string]bool{"a": true, "b": true, "c": true, "d": true}, cl)

	// Trigger d (leaf) → just d.
	cl = downstreamClosure(waves, map[string]bool{"d": true})
	assert.Equal(t, map[string]bool{"d": true}, cl)
}

func TestBuildRerunSeed_RerunFailed(t *testing.T) {
	waves := diamondWaves()
	// Prior run: a,b succeeded; c failed; d never ran (pending/skipped).
	prior := []engine.StepState{
		{Name: "a", Status: "succeeded"},
		{Name: "b", Status: "succeeded"},
		{Name: "c", Status: "failed"},
		{Name: "d", Status: "skipped"},
	}
	outputs := map[string]engine.StepResult{
		"a": {StepName: "a", Success: true, Outputs: map[string]string{"v": "1"}},
		"b": {StepName: "b", Success: true},
	}
	// re-run-failed triggers = non-succeeded = {c, d}. closure = {c, d} (d downstream
	// of c too). seed = succeeded steps not in closure = {a, b}.
	triggers := map[string]bool{"c": true, "d": true}
	seed := buildRerunSeed(waves, prior, outputs, triggers)

	assert.Contains(t, seed, "a")
	assert.Contains(t, seed, "b")
	assert.NotContains(t, seed, "c", "failed step must re-run")
	assert.NotContains(t, seed, "d", "downstream of failure must re-run")
	assert.Equal(t, "1", seed["a"].Outputs["v"], "carried-over outputs preserved")
}

func TestBuildRerunSeed_RetryFromStep(t *testing.T) {
	waves := diamondWaves()
	// All succeeded previously; retry from b.
	prior := []engine.StepState{
		{Name: "a", Status: "succeeded"},
		{Name: "b", Status: "succeeded"},
		{Name: "c", Status: "succeeded"},
		{Name: "d", Status: "succeeded"},
	}
	// retry-from b → closure {b, d}. seed = {a, c} (succeeded, not in closure).
	seed := buildRerunSeed(waves, prior, map[string]engine.StepResult{}, map[string]bool{"b": true})
	assert.Contains(t, seed, "a")
	assert.Contains(t, seed, "c")
	assert.NotContains(t, seed, "b", "chosen step re-runs")
	assert.NotContains(t, seed, "d", "downstream of chosen step re-runs")
}
