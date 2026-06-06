package engine

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Compile-time proof the docker executor satisfies the seam.
var _ StepExecutor = (*dockerExecutor)(nil)

func TestDockerExecutor_Kind(t *testing.T) {
	assert.Equal(t, "docker", NewDockerExecutor("docker", "", nil).Kind())
}

func TestContainerName(t *testing.T) {
	// Long run IDs are shortened; step names are sanitized to a runtime-valid form.
	name := containerName("run-abcdef123456", "Build & Test")
	assert.True(t, strings.HasPrefix(name, "flint-run-abcd-"), "got %q", name)
	assert.NotContains(t, name, " ")
	assert.NotContains(t, name, "&")
}

// buildRunArgs is the daemon-free heart of the executor — assert it without
// needing a container runtime.
func TestDockerExecutor_BuildRunArgs(t *testing.T) {
	e := NewDockerExecutor("podman", "/tmp/ws", nil)
	step := claimedStep{
		name: "build", runID: "run-1", env: map[string]string{"FOO": "bar"},
		repo: "acme/app", commitSHA: "deadbeef",
	}
	def := pipeline.Step{Name: "build", Image: "alpine:3.19", Run: pipeline.Cmd("echo hi")}

	args := e.buildRunArgs(step, def, "flint-run-1-build", "/tmp/ws/flint-run-1", "alpine:3.19")
	joined := strings.Join(args, " ")

	assert.Equal(t, "run", args[0])
	assert.Contains(t, joined, "--rm")
	assert.Contains(t, joined, "-v /tmp/ws/flint-run-1:/workspace")
	assert.Contains(t, joined, "-w /workspace")
	assert.Contains(t, joined, "-e FOO=bar")
	assert.Contains(t, joined, "-e FLINT_GIT_REPO=acme/app")
	assert.Contains(t, joined, "-e FLINT_GIT_SHA=deadbeef")
	// Image then the shell command come last.
	assert.Equal(t, []string{"alpine:3.19", "/bin/sh", "-c", "echo hi"}, args[len(args)-4:])
}

func TestDockerExecutor_MissingImage(t *testing.T) {
	e := NewDockerExecutor("docker", t.TempDir(), nil)
	def, _ := json.Marshal(pipeline.Step{Name: "x", Run: pipeline.Cmd("echo hi")})
	_, err := e.Dispatch(context.Background(), claimedStep{name: "x", execType: "run", stepDef: def, runID: "run-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no container image")
}

// TestDockerExecutor_RunsContainer is an opt-in end-to-end check: it actually
// runs a container. It only runs when FLINT_TEST_DOCKER=1 and a usable runtime
// is present, so CI stays hermetic and image pulls don't creep into normal runs.
func TestDockerExecutor_RunsContainer(t *testing.T) {
	if os.Getenv("FLINT_TEST_DOCKER") != "1" {
		t.Skip("set FLINT_TEST_DOCKER=1 (and have docker/podman) to run the container e2e")
	}
	runtime, err := DetectContainerRuntime()
	require.NoError(t, err)
	if out, err := exec.Command(runtime, "info").CombinedOutput(); err != nil {
		t.Skipf("container runtime not usable: %v: %s", err, out)
	}

	done := make(chan StepResult, 1)
	e := NewDockerExecutor(runtime, t.TempDir(), func(_ context.Context, _ string, r StepResult) error {
		done <- r
		return nil
	})
	def, _ := json.Marshal(pipeline.Step{Name: "echo", Image: "alpine:3.19", Run: pipeline.Cmd("echo hello")})
	_, err = e.Dispatch(context.Background(), claimedStep{name: "echo", execType: "run", stepDef: def, runID: "run-1"})
	require.NoError(t, err)

	select {
	case r := <-done:
		assert.True(t, r.Success)
		assert.Equal(t, 0, r.ExitCode)
	case <-time.After(120 * time.Second):
		t.Fatal("timed out waiting for container step (image pull may be slow)")
	}
}
