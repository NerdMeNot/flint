package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/NerdMeNot/flint/pkg/pipeline"
	"github.com/rs/zerolog/log"
)

// CompleteFunc reports a step's terminal result back to the engine. The local
// executor invokes it when a step process exits — standing in for the agent's
// /internal/complete callback that the k8s executor relies on.
type CompleteFunc func(ctx context.Context, taskToken string, result StepResult) error

// localExecutor runs steps as local OS processes against a per-run temp-dir
// workspace. It needs no Kubernetes, which makes it the basis for cluster-free
// development and tests. It does not isolate steps or pull images — the step
// image is ignored; the local Docker executor (Phase B) adds image fidelity.
type localExecutor struct {
	workspaceRoot string       // base dir for per-run workspaces ("" => os.TempDir())
	complete      CompleteFunc // invoked when a step finishes
}

// NewLocalExecutor builds the local subprocess executor. complete is called with
// each step's result when its process exits; it is required.
func NewLocalExecutor(workspaceRoot string, complete CompleteFunc) *localExecutor {
	return &localExecutor{workspaceRoot: workspaceRoot, complete: complete}
}

func (e *localExecutor) Kind() string { return "local" }

// Dispatch starts the step's command as a local process and returns a handle.
// It is fire-and-forget: the process runs in a goroutine and its result is
// reported via the complete callback, mirroring the k8s agent's behaviour.
func (e *localExecutor) Dispatch(ctx context.Context, step claimedStep) (string, error) {
	var stepDef pipeline.Step
	if err := json.Unmarshal(step.stepDef, &stepDef); err != nil {
		return "", fmt.Errorf("engine: unmarshal step def: %w", err)
	}

	root := e.workspaceRoot
	if root == "" {
		root = os.TempDir()
	}
	wsDir := filepath.Join(root, "flint-"+step.runID)
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		return "", fmt.Errorf("engine: create workspace dir: %w", err)
	}

	handle := step.runID + "/" + step.name
	go e.run(step, stepDef.Run.String(), wsDir)
	return handle, nil
}

// run executes the command, builds a StepResult from its exit status, and
// reports it through the complete callback.
func (e *localExecutor) run(step claimedStep, command, wsDir string) {
	ctx := context.Background()

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Dir = wsDir
	cmd.Env = append(os.Environ(), localStepEnv(step)...)
	out, runErr := cmd.CombinedOutput()

	result := resultFromExit(step.name, runErr)

	log.Info().Str("step", step.name).Str("kind", "local").
		Bool("ok", result.Success).Int("exit", result.ExitCode).
		Msg("engine: local step finished")
	log.Debug().Str("step", step.name).Str("output", string(out)).Msg("engine: local step output")

	if e.complete != nil {
		if err := e.complete(ctx, step.taskToken, result); err != nil {
			log.Error().Err(err).Str("step", step.name).Msg("engine: local completion callback failed")
		}
	}
}

// resultFromExit builds a StepResult from a command's run error (nil => success).
// Shared by the local and docker executors, which both run a process and report
// its terminal status via a CompleteFunc.
func resultFromExit(stepName string, runErr error) StepResult {
	res := StepResult{StepName: stepName, Success: runErr == nil}
	if runErr != nil {
		res.Error = runErr.Error()
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			res.ExitCode = exitErr.ExitCode()
		}
	}
	return res
}

// localStepEnv renders the step's merged env map as KEY=VALUE entries.
func localStepEnv(step claimedStep) []string {
	env := make([]string, 0, len(step.env))
	for k, v := range step.env {
		env = append(env, k+"="+v)
	}
	return env
}
