package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/rs/zerolog/log"
)

// dockerExecutor runs steps as local containers via a Docker-compatible CLI
// (Docker or Podman). It gives faithful cluster-free execution — the real step
// image, real isolation — without Kubernetes, an agent, or a sidecar: it runs
// `<runtime> run` directly and reports completion through a CompleteFunc. The
// CLI honours DOCKER_HOST, so it works against a Podman socket unchanged.
type dockerExecutor struct {
	runtime       string       // container CLI binary, e.g. "docker" or "podman"
	workspaceRoot string       // host base dir bind-mounted to /workspace ("" => os.TempDir())
	complete      CompleteFunc // invoked when a step container exits
}

// NewDockerExecutor builds the container executor. runtime is the CLI binary to
// invoke; use DetectContainerRuntime to pick a default.
func NewDockerExecutor(runtime, workspaceRoot string, complete CompleteFunc) *dockerExecutor {
	return &dockerExecutor{runtime: runtime, workspaceRoot: workspaceRoot, complete: complete}
}

// DetectContainerRuntime returns the first available container CLI, preferring
// docker (which may itself be a podman shim) then podman.
func DetectContainerRuntime() (string, error) {
	for _, name := range []string{"docker", "podman"} {
		if path, err := exec.LookPath(name); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("no container runtime found (need docker or podman on PATH)")
}

func (e *dockerExecutor) Kind() string { return "docker" }

// Dispatch starts the step's image as a container and returns the container name
// as its handle. Fire-and-forget: the container runs in a goroutine and its
// result is reported via the complete callback.
func (e *dockerExecutor) Dispatch(ctx context.Context, step claimedStep) (string, error) {
	var stepDef pipeline.Step
	if err := json.Unmarshal(step.stepDef, &stepDef); err != nil {
		return "", fmt.Errorf("engine: unmarshal step def: %w", err)
	}

	image := stepDef.Image
	if image == "" {
		image = step.pipelineImage
	}
	if image == "" {
		return "", fmt.Errorf("engine: step %q has no container image", step.name)
	}

	root := e.workspaceRoot
	if root == "" {
		root = os.TempDir()
	}
	wsDir := filepath.Join(root, "flint-"+step.runID)
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		return "", fmt.Errorf("engine: create workspace dir: %w", err)
	}

	name := containerName(step.runID, step.name)
	args := e.buildRunArgs(step, stepDef, name, wsDir, image)

	go e.run(step, name, args)
	return name, nil
}

// buildRunArgs assembles the `<runtime> run …` arguments for a step.
func (e *dockerExecutor) buildRunArgs(step claimedStep, stepDef pipeline.Step, name, wsDir, image string) []string {
	args := []string{"run", "--rm", "--name", name,
		"-v", wsDir + ":/workspace", "-w", "/workspace"}
	for k, v := range step.env {
		args = append(args, "-e", k+"="+v)
	}
	// Surface git/run context for parity with the k8s executor's agent env.
	for k, v := range map[string]string{
		"FLINT_RUN_ID":   step.runID,
		"FLINT_GIT_REPO": step.repo,
		"FLINT_GIT_REF":  step.ref,
		"FLINT_GIT_SHA":  step.commitSHA,
	} {
		if v != "" {
			args = append(args, "-e", k+"="+v)
		}
	}
	return append(args, image, "/bin/sh", "-c", stepDef.Run.String())
}

func (e *dockerExecutor) run(step claimedStep, name string, args []string) {
	ctx := context.Background()
	out, runErr := exec.CommandContext(ctx, e.runtime, args...).CombinedOutput()

	result := resultFromExit(step.name, runErr)
	log.Info().Str("step", step.name).Str("kind", "docker").Str("container", name).
		Bool("ok", result.Success).Int("exit", result.ExitCode).
		Msg("engine: container step finished")
	log.Debug().Str("step", step.name).Str("output", string(out)).Msg("engine: container step output")

	if e.complete != nil {
		if err := e.complete(ctx, step.taskToken, result); err != nil {
			log.Error().Err(err).Str("step", step.name).Msg("engine: docker completion callback failed")
		}
	}
}

// containerName builds a stable, runtime-valid container name for a step.
func containerName(runID, stepName string) string {
	short := runID
	if len(short) > 8 {
		short = short[:8]
	}
	return fmt.Sprintf("flint-%s-%s", short, sanitizeK8sName(stepName))
}
