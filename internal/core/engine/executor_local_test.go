package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time proof that both executors satisfy the seam — the local executor
// is a genuinely different (non-Kubernetes) implementation, which is the point:
// it validates that StepExecutor isn't accidentally container-shaped.
var (
	_ StepExecutor = (*k8sExecutor)(nil)
	_ StepExecutor = (*localExecutor)(nil)
	_ stepCleaner  = (*k8sExecutor)(nil)
)

func stepWithRun(name, command string) claimedStep {
	def, _ := json.Marshal(pipeline.Step{Name: name, Run: pipeline.Cmd(command)})
	return claimedStep{
		name:      name,
		execType:  "run",
		runID:     "run-abcdef12",
		taskToken: "task-token",
		stepDef:   def,
	}
}

func dispatchAndWait(t *testing.T, command string) StepResult {
	t.Helper()
	done := make(chan StepResult, 1)
	exec := NewLocalExecutor(t.TempDir(), func(_ context.Context, token string, result StepResult) error {
		assert.Equal(t, "task-token", token, "completion should carry the step's task token")
		done <- result
		return nil
	})

	handle, err := exec.Dispatch(context.Background(), stepWithRun("build", command))
	require.NoError(t, err)
	require.NotEmpty(t, handle, "Dispatch should return a correlation handle")

	select {
	case res := <-done:
		return res
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for local step completion")
		return StepResult{}
	}
}

func TestLocalExecutor_DispatchSuccess(t *testing.T) {
	res := dispatchAndWait(t, "echo hello")
	assert.True(t, res.Success)
	assert.Equal(t, 0, res.ExitCode)
	assert.Equal(t, "build", res.StepName)
}

func TestLocalExecutor_DispatchFailureExitCode(t *testing.T) {
	res := dispatchAndWait(t, "exit 3")
	assert.False(t, res.Success)
	assert.Equal(t, 3, res.ExitCode)
	assert.NotEmpty(t, res.Error)
}

func TestLocalExecutor_Kind(t *testing.T) {
	assert.Equal(t, "local", NewLocalExecutor("", nil).Kind())
}

// dispatchStep routes by exec type via the registry, independently of executor
// implementation.
func TestDispatchStep_Routing(t *testing.T) {
	exec := NewLocalExecutor(t.TempDir(), func(context.Context, string, StepResult) error { return nil })
	registry := ExecutorRegistry{"run": exec}

	// Gates/approvals don't execute — no handle, no error, executor not invoked.
	handle, err := dispatchStep(context.Background(), registry, claimedStep{name: "approve", execType: "gate"})
	require.NoError(t, err)
	assert.Empty(t, handle)

	// A registered type dispatches.
	h, err := dispatchStep(context.Background(), registry, stepWithRun("r", "true"))
	require.NoError(t, err)
	assert.NotEmpty(t, h)

	// An unregistered type returns errNoExecutor (the loop leaves it running).
	_, err = dispatchStep(context.Background(), registry, claimedStep{name: "weird", execType: "nope"})
	require.ErrorIs(t, err, errNoExecutor)
}
