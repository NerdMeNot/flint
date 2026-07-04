package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// steps.go — the in-pod steps driver.
//
// A ci-dialect job compiles to ONE engine step of exec type "steps": a single
// pod whose sub-steps run sequentially, sharing the image and the workspace.
// The driver is this binary (static, CGO-free) copied into the shared
// workspace by the init container and executed as the step container's main
// process inside the USER'S image. It owns:
//
//   - sequential sub-step execution with per-sub-step env, workingDir, shell,
//     timeout, retry, and continueOnError
//   - in-pod if: evaluation against {git, run, env, needs, steps.outputs}
//   - $FLINT_OUTPUT capture per sub-step → steps.outputs.* for later sub-steps
//   - job-level declared outputs (name → ${{ }} expression), evaluated after
//     the sub-steps and written to the emit file for the sidecar to report
//   - the same pod contract as the shell wrapper: combined output appended to
//     .flint-step.log, exit code written to .flint-exit — the watch sidecar
//     needs no special handling for group steps.

// StepsConfig is the driver's environment-supplied configuration.
type StepsConfig struct {
	Workspace    string
	Steps        []SubStep
	JobOutputs   map[string]string            // name → ${{ }} expression
	NeedsOutputs map[string]map[string]string // base job name → outputs
	Git          map[string]any               // git namespace (sha, branch, repoUrl)
	Run          map[string]any               // run namespace (id, trigger)
}

// SubStep mirrors the fields of a compiled sub-step the driver executes. It
// decodes directly from the engine's step_def sub-step JSON (pipeline.Step).
type SubStep struct {
	Name            string              `json:"name"`
	Run             pipeline.RunCommand `json:"run"`
	Env             map[string]string   `json:"env"`
	If              string              `json:"if"`
	Shell           string              `json:"shell"`
	WorkingDir      string              `json:"workingDir"`
	Timeout         string              `json:"timeout"`
	ContinueOnError bool                `json:"continueOnError"`
	Retry           *pipeline.RetrySpec `json:"retry"`
}

// InstallDriver copies the running flint-agent binary into the shared
// workspace at .flint-bin/flint-agent so the step container (the user's
// image) can exec the steps driver. Called by the init container for group
// steps; the sidecar removes the directory before workspace sync-out.
func InstallDriver(workspace string) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("agent: locate own binary: %w", err)
	}
	src, err := os.Open(self)
	if err != nil {
		return fmt.Errorf("agent: open own binary: %w", err)
	}
	defer src.Close()

	binDir := filepath.Join(workspace, ".flint-bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return fmt.Errorf("agent: create driver dir: %w", err)
	}
	dstPath := filepath.Join(binDir, "flint-agent")
	dst, err := os.OpenFile(dstPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return fmt.Errorf("agent: create driver copy: %w", err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return fmt.Errorf("agent: copy driver: %w", err)
	}
	return dst.Close()
}

// LoadStepsConfig reads the driver configuration from the environment.
func LoadStepsConfig() (*StepsConfig, error) {
	cfg := &StepsConfig{
		Workspace: os.Getenv("FLINT_WORKSPACE"),
		Git: map[string]any{
			"sha":     os.Getenv("FLINT_GIT_SHA"),
			"branch":  os.Getenv("FLINT_GIT_REF"),
			"repoUrl": os.Getenv("FLINT_GIT_REPO"),
		},
		Run: map[string]any{
			"id":      os.Getenv("FLINT_RUN_ID"),
			"trigger": os.Getenv("FLINT_TRIGGER_TYPE"),
		},
	}
	if cfg.Workspace == "" {
		cfg.Workspace = "/workspace"
	}
	raw := os.Getenv("FLINT_STEPS_SPEC")
	if raw == "" {
		return nil, fmt.Errorf("agent: FLINT_STEPS_SPEC is required for the steps driver")
	}
	if err := json.Unmarshal([]byte(raw), &cfg.Steps); err != nil {
		return nil, fmt.Errorf("agent: FLINT_STEPS_SPEC is not valid JSON: %w", err)
	}
	if raw := os.Getenv("FLINT_JOB_OUTPUTS"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg.JobOutputs); err != nil {
			return nil, fmt.Errorf("agent: FLINT_JOB_OUTPUTS is not valid JSON: %w", err)
		}
	}
	if raw := os.Getenv("FLINT_NEEDS_OUTPUTS"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg.NeedsOutputs); err != nil {
			return nil, fmt.Errorf("agent: FLINT_NEEDS_OUTPUTS is not valid JSON: %w", err)
		}
	}
	return cfg, nil
}

// RunSteps executes the sub-steps and honours the pod contract (.flint-step.log,
// .flint-exit, .flint-emit). It returns an error only for driver-level failures;
// sub-step failures are reported through the exit file.
func RunSteps(ctx context.Context, cfg *StepsConfig) error {
	logPath := filepath.Join(cfg.Workspace, ".flint-step.log")
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("agent: open step log: %w", err)
	}
	defer logFile.Close()
	out := io.MultiWriter(os.Stdout, logFile)

	exitCode := runSubSteps(ctx, cfg, out)

	// Pod contract: the sidecar polls .flint-exit for completion.
	exitPath := filepath.Join(cfg.Workspace, ".flint-exit")
	if err := os.WriteFile(exitPath, []byte(fmt.Sprintf("%d", exitCode)), 0o644); err != nil {
		return fmt.Errorf("agent: write exit file: %w", err)
	}
	if exitCode != 0 {
		return fmt.Errorf("agent: job failed with exit code %d", exitCode)
	}
	return nil
}

func runSubSteps(ctx context.Context, cfg *StepsConfig, out io.Writer) int {
	stepOutputs := map[string]string{} // steps.outputs.* (job-scoped, flat)
	failedCode := 0

	for i, s := range cfg.Steps {
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("step %d", i+1)
		}

		if failedCode != 0 {
			fmt.Fprintf(out, "\n──— %s — skipped (previous step failed) ———\n", name)
			continue
		}

		// if: — evaluated in-pod so it can see earlier sub-steps' outputs. An
		// eval ERROR fails the job loudly (same policy as the engine).
		if s.If != "" {
			exprCtx := cfg.exprContext(stepOutputs)
			shouldRun, evalErr := pipeline.EvalCondition(s.If, exprCtx)
			if evalErr != nil {
				fmt.Fprintf(out, "\n──— %s — FAILED: if condition %q: %v ———\n", name, s.If, evalErr)
				failedCode = 1
				continue
			}
			if !shouldRun {
				fmt.Fprintf(out, "\n──— %s — skipped (if condition false) ———\n", name)
				continue
			}
		}

		fmt.Fprintf(out, "\n──— %s ———\n", name)

		attempts := 1
		var delay time.Duration
		if s.Retry != nil && s.Retry.Attempts > 1 {
			attempts = s.Retry.Attempts
			if d, err := time.ParseDuration(s.Retry.Delay); err == nil {
				delay = d
			}
		}

		var rc int
		for attempt := 0; attempt < attempts; attempt++ {
			if attempt > 0 {
				fmt.Fprintf(out, "──— %s — retry %d/%d after %s ———\n", name, attempt+1, attempts, delay)
				time.Sleep(delay)
			}
			rc = runOneSubStep(ctx, cfg, s, i, stepOutputs, out)
			if rc == 0 {
				break
			}
		}

		if rc != 0 {
			if s.ContinueOnError {
				fmt.Fprintf(out, "──— %s — failed with exit code %d (continueOnError) ———\n", name, rc)
				continue
			}
			fmt.Fprintf(out, "──— %s — failed with exit code %d ———\n", name, rc)
			failedCode = rc
		}
	}

	// Job-level declared outputs: evaluated against the final steps.outputs
	// and written to the emit file (key=value) for the sidecar to report.
	// Only declared outputs cross the job boundary.
	if failedCode == 0 && len(cfg.JobOutputs) > 0 {
		if err := cfg.writeJobOutputs(stepOutputs, out); err != nil {
			fmt.Fprintf(out, "\nFAILED evaluating job outputs: %v\n", err)
			failedCode = 1
		}
	}

	return failedCode
}

func runOneSubStep(ctx context.Context, cfg *StepsConfig, s SubStep, idx int, stepOutputs map[string]string, out io.Writer) int {
	timeout := time.Hour
	if s.Timeout != "" {
		if d, err := time.ParseDuration(s.Timeout); err == nil && d > 0 {
			timeout = d
		}
	}
	stepCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command := s.Run.String()
	// emit() is the canonical step-output helper (`emit key value`) — the
	// same contract single-run steps get from their shell wrapper.
	const emitFn = `emit() { echo "$1=$2" >> "$FLINT_OUTPUT"; }; `
	var cmd *exec.Cmd
	switch s.Shell {
	case "bash":
		cmd = exec.CommandContext(stepCtx, "bash", "-c", emitFn+command)
	case "python":
		cmd = exec.CommandContext(stepCtx, "python3", "-c", command)
	default:
		cmd = exec.CommandContext(stepCtx, "/bin/sh", "-c", emitFn+command)
	}

	cmd.Dir = cfg.Workspace
	if s.WorkingDir != "" {
		if filepath.IsAbs(s.WorkingDir) {
			cmd.Dir = s.WorkingDir
		} else {
			cmd.Dir = filepath.Join(cfg.Workspace, s.WorkingDir)
		}
	}

	// Per-sub-step $FLINT_OUTPUT file; parsed into steps.outputs afterwards.
	outputFile := filepath.Join(cfg.Workspace, fmt.Sprintf(".flint-output-%d", idx))
	env := cfg.interpolatedEnviron(stepOutputs)
	for k, v := range s.Env {
		env = append(env, k+"="+cfg.interpolate(v, stepOutputs))
	}
	env = append(env, "FLINT_OUTPUT="+outputFile)
	cmd.Env = env
	cmd.Stdout = out
	cmd.Stderr = out

	err := cmd.Run()
	collectOutputs(outputFile, stepOutputs)
	_ = os.Remove(outputFile)

	if err == nil {
		return 0
	}
	if stepCtx.Err() == context.DeadlineExceeded {
		fmt.Fprintf(out, "step timed out after %s\n", timeout)
		return 124
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	fmt.Fprintf(out, "failed to start step: %v\n", err)
	return 127
}

// exprContext builds the in-pod expression context. It mirrors the engine's
// runtime namespaces (git, run, env, needs) plus the job-scoped
// steps.outputs.* accumulated from earlier sub-steps.
func (cfg *StepsConfig) exprContext(stepOutputs map[string]string) pipeline.ExprContext {
	envNS := map[string]any{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			envNS[k] = v
		}
	}
	outputs := map[string]any{}
	for k, v := range stepOutputs {
		outputs[k] = v
	}
	needs := map[string]any{}
	for job, outs := range cfg.NeedsOutputs {
		jobOuts := map[string]any{}
		for k, v := range outs {
			jobOuts[k] = v
		}
		needs[job] = map[string]any{"outputs": jobOuts, "result": "success"}
	}
	return pipeline.ExprContext{
		"git":   cfg.Git,
		"run":   cfg.Run,
		"env":   envNS,
		"needs": needs,
		"steps": map[string]any{"outputs": outputs},
	}
}

// interpolate resolves ${{ }} templates in a string against the in-pod context.
// On error the original string is kept (the subsequent command will surface it).
func (cfg *StepsConfig) interpolate(s string, stepOutputs map[string]string) string {
	if !strings.Contains(s, "${{") {
		return s
	}
	resolved, err := pipeline.Interpolate(s, cfg.exprContext(stepOutputs))
	if err != nil {
		return s
	}
	return resolved
}

// interpolatedEnviron returns the pod environment with ${{ }} values resolved —
// job-level env like VERSION: ${{ needs.build.outputs.version }} reaches the
// sub-step processes with the actual value.
func (cfg *StepsConfig) interpolatedEnviron(stepOutputs map[string]string) []string {
	environ := os.Environ()
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		k, v, ok := strings.Cut(kv, "=")
		if ok && strings.Contains(v, "${{") {
			out = append(out, k+"="+cfg.interpolate(v, stepOutputs))
			continue
		}
		out = append(out, kv)
	}
	return out
}

// writeJobOutputs evaluates the declared job outputs and appends them to the
// emit file (the sidecar reports emit-file contents as the step's outputs).
func (cfg *StepsConfig) writeJobOutputs(stepOutputs map[string]string, out io.Writer) error {
	// $FLINT_OUTPUT is the job-level output channel (the daemon reads it from
	// the step IO dir); the workspace .flint-emit path is the pod-era default.
	emitPath := os.Getenv("FLINT_OUTPUT")
	if emitPath == "" {
		emitPath = filepath.Join(cfg.Workspace, ".flint-emit")
	}
	f, err := os.OpenFile(emitPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	ctx := cfg.exprContext(stepOutputs)
	for name, expr := range cfg.JobOutputs {
		val, err := pipeline.Interpolate(expr, ctx)
		if err != nil {
			return fmt.Errorf("output %q (%s): %w", name, expr, err)
		}
		if _, err := fmt.Fprintf(f, "%s=%s\n", name, val); err != nil {
			return err
		}
		fmt.Fprintf(out, "output %s=%s\n", name, val)
	}
	return nil
}

// collectOutputs parses key=value lines from a $FLINT_OUTPUT file into the
// job-scoped outputs map.
func collectOutputs(path string, into map[string]string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && k != "" {
			into[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
}
