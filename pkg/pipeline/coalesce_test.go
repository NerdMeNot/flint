package pipeline

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoalesceSteps_NoRunner(t *testing.T) {
	p := &Pipeline{
		Steps: []Step{
			{Name: "install", Run: Cmd("npm install")},
			{Name: "build", Run: Cmd("npm run build")},
		},
	}
	got := CoalesceSteps(p)
	assert.Same(t, p, got, "no runner: should return same pointer")
	assert.Len(t, got.Steps, 2)
}

func TestCoalesceSteps_WithDependsOn(t *testing.T) {
	p := &Pipeline{
		Runner: "ubuntu",
		Steps: []Step{
			{Name: "install", Run: Cmd("npm install")},
			{Name: "build", Run: Cmd("npm run build"), DependsOn: []string{"install"}},
		},
	}
	got := CoalesceSteps(p)
	assert.Same(t, p, got, "explicit dependsOn: should return same pointer unchanged")
	assert.Len(t, got.Steps, 2)
}

func TestCoalesceSteps_AllRunSteps(t *testing.T) {
	p := &Pipeline{
		Runner: "ubuntu",
		Steps: []Step{
			{Name: "install", Run: Cmd("npm install")},
			{Name: "build", Run: Cmd("npm run build")},
			{Name: "test", Run: Cmd("npm test")},
		},
	}
	got := CoalesceSteps(p)
	require.Len(t, got.Steps, 1, "all coalesceable: should collapse to one wrapper")

	wrapper := got.Steps[0]
	assert.Equal(t, "install", wrapper.Name)
	assert.Equal(t, "ubuntu", wrapper.Runner)
	assert.Equal(t, "steps", wrapper.ExecType())
	require.Len(t, wrapper.Steps, 3)
	assert.Equal(t, "install", wrapper.Steps[0].Name)
	assert.Equal(t, "build", wrapper.Steps[1].Name)
	assert.Equal(t, "test", wrapper.Steps[2].Name)

	// Nested steps should have runner cleared (inherited from wrapper).
	for _, ns := range wrapper.Steps {
		assert.Empty(t, ns.Runner)
	}
}

func TestCoalesceSteps_GateBreaksGroup(t *testing.T) {
	gate := &Gate{Approvers: []string{"team"}}
	p := &Pipeline{
		Runner: "ubuntu",
		Steps: []Step{
			{Name: "build", Run: Cmd("make build")},
			{Name: "test", Run: Cmd("make test")},
			{Name: "approve", Gate: gate},
			{Name: "deploy", Run: Cmd("./deploy.sh")},
			{Name: "verify", Run: Cmd("./verify.sh")},
		},
	}
	got := CoalesceSteps(p)
	require.Len(t, got.Steps, 3)

	assert.Equal(t, "build", got.Steps[0].Name)
	assert.Equal(t, "steps", got.Steps[0].ExecType())
	assert.Len(t, got.Steps[0].Steps, 2)

	assert.Equal(t, "approve", got.Steps[1].Name)
	assert.Equal(t, "gate", got.Steps[1].ExecType())

	assert.Equal(t, "deploy", got.Steps[2].Name)
	assert.Equal(t, "steps", got.Steps[2].ExecType())
	assert.Len(t, got.Steps[2].Steps, 2)
}

func TestCoalesceSteps_SingleStepGroupPassthrough(t *testing.T) {
	gate := &Gate{Approvers: []string{"team"}}
	p := &Pipeline{
		Runner: "ubuntu",
		Steps: []Step{
			{Name: "build", Run: Cmd("make build")},
			{Name: "approve", Gate: gate},
			{Name: "deploy", Run: Cmd("./deploy.sh")},
		},
	}
	got := CoalesceSteps(p)
	require.Len(t, got.Steps, 3, "single-step groups are not wrapped")

	// build and deploy are single-step groups — left as plain run steps.
	assert.Equal(t, "run", got.Steps[0].ExecType())
	assert.Equal(t, "gate", got.Steps[1].ExecType())
	assert.Equal(t, "run", got.Steps[2].ExecType())
}

func TestCoalesceSteps_PerStepRunnerOverride(t *testing.T) {
	p := &Pipeline{
		Runner: "ubuntu",
		Steps: []Step{
			{Name: "build", Run: Cmd("make build")},
			{Name: "gpu-test", Run: Cmd("pytest"), Runner: "gpu"},
			{Name: "lint", Run: Cmd("golangci-lint run")},
		},
	}
	got := CoalesceSteps(p)
	// gpu-test breaks the group; build and lint are single-step groups (not wrapped).
	require.Len(t, got.Steps, 3)
	assert.Equal(t, "run", got.Steps[0].ExecType(), "build: single-step, not wrapped")
	assert.Equal(t, "gpu", got.Steps[1].Runner, "gpu-test: runner preserved")
	assert.Equal(t, "run", got.Steps[2].ExecType(), "lint: single-step, not wrapped")
}

func TestCoalesceSteps_NestedStepsNotCoalesced(t *testing.T) {
	p := &Pipeline{
		Runner: "ubuntu",
		Steps: []Step{
			{Name: "setup", Steps: []Step{
				{Name: "a", Run: Cmd("echo a")},
				{Name: "b", Run: Cmd("echo b")},
			}},
			{Name: "build", Run: Cmd("make build")},
		},
	}
	// "setup" is already a steps: group — should not be coalesced with "build".
	got := CoalesceSteps(p)
	require.Len(t, got.Steps, 2)
	assert.Equal(t, "steps", got.Steps[0].ExecType())
	assert.Equal(t, "run", got.Steps[1].ExecType())
}

func TestCoalesceSteps_WhenOnFailureBreaksGroup(t *testing.T) {
	p := &Pipeline{
		Runner: "ubuntu",
		Steps: []Step{
			{Name: "build", Run: Cmd("make build")},
			{Name: "test", Run: Cmd("make test")},
			{Name: "cleanup", Run: Cmd("./cleanup.sh"), When: "always"},
			{Name: "notify", Run: Cmd("./notify.sh"), When: "onFailure"},
		},
	}
	// build+test can coalesce (both default when:), cleanup and notify cannot.
	got := CoalesceSteps(p)
	require.Len(t, got.Steps, 3)
	assert.Equal(t, "steps", got.Steps[0].ExecType(), "build+test: wrapped")
	assert.Len(t, got.Steps[0].Steps, 2)
	assert.Equal(t, "cleanup", got.Steps[1].Name)
	assert.Equal(t, "run", got.Steps[1].ExecType(), "cleanup: not wrapped (when: always)")
	assert.Equal(t, "notify", got.Steps[2].Name)
	assert.Equal(t, "run", got.Steps[2].ExecType(), "notify: not wrapped (when: onFailure)")
}

func TestCoalesceSteps_PipelineRunnerPreserved(t *testing.T) {
	p := &Pipeline{
		Runner: "ubuntu",
		Steps: []Step{
			{Name: "a", Run: Cmd("echo a")},
			{Name: "b", Run: Cmd("echo b")},
		},
	}
	got := CoalesceSteps(p)
	assert.Equal(t, "ubuntu", got.Runner, "pipeline-level Runner should be preserved")
}
