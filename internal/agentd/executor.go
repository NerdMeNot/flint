package agentd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/types/known/timestamppb"

	agentruntime "github.com/NerdMeNot/flint/internal/agentd/runtime"
	"github.com/NerdMeNot/flint/pkg/pipeline"
	agentv1 "github.com/NerdMeNot/flint/protogen/agent/v1"
)

// execution tracks one running assignment for cancellation.
type execution struct {
	assignmentID string
	runID        string
	stepName     string
	stop         context.CancelFunc
}

func (e *execution) cancel(context.Context) { e.stop() }

// reportTimeout bounds the whole completion-report retry loop. Generous enough
// to ride out a brief server blip, bounded so a shutdown cannot hang on an
// unreachable control plane.
const reportTimeout = 25 * time.Second

// execute runs one assignment end to end: prepare dirs, run the step through
// the runtime while streaming logs, then report the outcome. Errors never
// escape — every path ends in a completion report (or the server's lease
// machinery takes over).
func (d *Daemon) execute(ctx context.Context, a *agentv1.Assignment) {
	payload := a.GetPayload()
	logger := log.With().Str("assignment", a.GetAssignmentId()).
		Str("run", a.GetRunId()).Str("step", a.GetStepName()).Logger()

	stepCtx, stop := context.WithCancel(ctx)
	defer stop()
	if t := payload.GetTimeoutSeconds(); t > 0 {
		var cancelT context.CancelFunc
		stepCtx, cancelT = context.WithTimeout(stepCtx, time.Duration(t)*time.Second)
		defer cancelT()
	}

	ex := &execution{
		assignmentID: a.GetAssignmentId(),
		runID:        a.GetRunId(),
		stepName:     a.GetStepName(),
		stop:         stop,
	}
	d.mu.Lock()
	d.active[ex.assignmentID] = ex
	d.mu.Unlock()
	defer func() {
		d.mu.Lock()
		delete(d.active, ex.assignmentID)
		d.mu.Unlock()
	}()

	started := time.Now()
	result := d.runStep(stepCtx, a, logger)
	result.StartedAt = timestamppb.New(started)
	result.FinishedAt = timestamppb.New(time.Now())
	result.DurationMs = time.Since(started).Milliseconds()

	// Completion is unary + retried: the engine must hear the outcome even if
	// the log stream died.
	//
	// Reported on a context detached from the caller's. The result is work that
	// has already been done — cancelling its delivery does not undo the step, it
	// only hides it, and the engine then sweeps the step at its deadline and (now
	// that a timeout honours the retry policy) runs it all over again. Shutdown
	// waits for this via the daemon's execution WaitGroup.
	reportCtx, cancelReport := context.WithTimeout(context.WithoutCancel(ctx), reportTimeout)
	defer cancelReport()

	for attempt := range 3 {
		_, err := d.client.svc.ReportStepComplete(reportCtx, &agentv1.ReportStepCompleteRequest{
			AssignmentId: ex.assignmentID,
			MachineId:    d.id.MachineID,
			TaskToken:    payload.GetTaskToken(),
			Result:       result,
		})
		if err == nil {
			logger.Info().Str("status", result.GetStatus()).Int32("exit", result.GetExitCode()).
				Msg("agentd: step reported")
			return
		}
		logger.Warn().Err(err).Int("attempt", attempt+1).Msg("agentd: completion report failed")
		select {
		case <-reportCtx.Done():
			return
		case <-time.After(time.Duration(1<<attempt) * time.Second):
		}
	}
	logger.Error().Msg("agentd: completion report exhausted retries — server sweep will recover")
}

// runStep executes the step and returns its result (no error paths escape).
func (d *Daemon) runStep(ctx context.Context, a *agentv1.Assignment, logger zerolog.Logger) *agentv1.StepResult {
	payload := a.GetPayload()

	var stepDef pipeline.Step
	if err := json.Unmarshal(payload.GetStepDefJson(), &stepDef); err != nil {
		return failResult("invalid step definition: " + err.Error())
	}

	wsDir := d.dirs.runWorkspace(a.GetRunId())
	ioDir := d.dirs.stepIO(a.GetRunId(), a.GetStepName())
	for _, dir := range []string{wsDir, ioDir} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return failResult("workspace setup: " + err.Error())
		}
	}
	emitFile := filepath.Join(ioDir, "emit")
	_ = os.Remove(emitFile) // retries start clean
	_ = os.Remove(filepath.Join(ioDir, "steps.json"))

	// Built-in checkout runs natively on the daemon: it owns the git mirrors,
	// so repeat builds pay the fetch delta instead of a full clone.
	if checkoutCommand(&stepDef) {
		return d.runCheckout(ctx, a, &stepDef, wsDir, logger)
	}

	// Data plane: artifact inputs land first, then the cache (its key may
	// hash files the artifacts brought in).
	if err := d.downloadArtifacts(ctx, payload, &stepDef, wsDir); err != nil {
		return failResult(err.Error())
	}
	d.restoreCache(ctx, payload, &stepDef, wsDir, logger)

	// Secrets: pulled at execution time, injected via a 0600 env file the
	// runtime sources into the step — never the daemon's own environment.
	secretsFile, cleanupSecrets, err := d.fetchSecrets(ctx, a, ioDir)
	if err != nil {
		return failResult(err.Error())
	}
	defer cleanupSecrets()

	command, err := buildCommand(payload, &stepDef)
	if err != nil {
		return failResult(err.Error())
	}

	env, err := buildEnv(payload, &stepDef)
	if err != nil {
		return failResult(err.Error())
	}

	agentBinary := d.cfg.AgentBinary
	if agentBinary == "" {
		agentBinary, _ = os.Executable()
	}

	// Per-step log stream: hello binds it, batches flow, cancellation comes
	// back. A dead stream degrades to no live logs — completion still lands
	// via the unary report.
	relay := newLogRelay(ctx, d.client, d.id.MachineID, a, func(reason string) {
		logger.Warn().Str("reason", reason).Msg("agentd: server cancelled step")
		d.cancelAssignment(ctx, a.GetAssignmentId(), reason)
	})
	defer relay.close()

	spec := agentruntime.StepSpec{
		RunID:          a.GetRunId(),
		StepName:       a.GetStepName(),
		Image:          firstNonEmpty(stepDef.Image, payload.GetPipelineImage()),
		Command:        command,
		Env:            env,
		SecretsEnvFile: secretsFile,
		WorkspaceDir:   wsDir,
		IODir:          ioDir,
		AgentBinary:    agentBinary,
		CPUMillis:      payload.GetResources().GetCpuMillis(),
		MemoryMB:       payload.GetResources().GetMemoryMb(),
		Privileged:     payload.GetResources().GetPrivileged(),
		Services:       serviceSpecs(stepDef.Services),
		Stdout:         relay.writer("stdout"),
		Stderr:         relay.writer("stderr"),
	}

	handle, err := d.rt.CreateStep(ctx, spec)
	if err != nil {
		return failResult("start step: " + err.Error())
	}
	defer d.rt.Remove(context.WithoutCancel(ctx), handle) //nolint:errcheck

	relay.started()

	exit, waitErr := d.rt.Wait(ctx, handle)
	if waitErr != nil {
		// Context death = timeout or cancellation: kill the process tree.
		_ = d.rt.Kill(context.WithoutCancel(ctx), handle, 10*time.Second)
		if ctx.Err() == context.DeadlineExceeded {
			return &agentv1.StepResult{Status: "timed_out", ExitCode: 124, ErrorMessage: "step timeout exceeded"}
		}
		return &agentv1.StepResult{Status: "cancelled", ExitCode: 130, ErrorMessage: "step cancelled"}
	}

	relay.flush()

	outputs := readEmits(emitFile)
	if exit.Code == 0 {
		// Post-success data plane: publish artifacts (failure here fails the
		// step — a missing declared artifact breaks consumers), save cache
		// (best-effort).
		if err := d.uploadArtifacts(ctx, payload, &stepDef, wsDir); err != nil {
			return failResult(err.Error())
		}
		d.saveCache(ctx, payload, &stepDef, wsDir, logger)
		return &agentv1.StepResult{Status: "succeeded", ExitCode: 0, Outputs: outputs}
	}
	return &agentv1.StepResult{
		Status: "failed", ExitCode: int32(exit.Code), Outputs: outputs,
		ErrorMessage: fmt.Sprintf("step exited with code %d", exit.Code),
	}
}

// buildCommand resolves the step into an argv. Run steps execute through a
// shell wrapper providing emit(); group ("steps") jobs run the steps driver —
// the agent binary itself, bind-mounted into the container.
func buildCommand(payload *agentv1.StepPayload, step *pipeline.Step) ([]string, error) {
	if payload.GetExecType() == "steps" || len(step.Steps) > 0 {
		return []string{agentruntime.AgentBinaryMount, "steps"}, nil
	}
	run := step.Run.String()
	if run == "" {
		return nil, fmt.Errorf("step %q has no run command (exec type %q not supported by this agent)", step.Name, step.ExecType())
	}
	// emit() appends to $FLINT_OUTPUT — the canonical step-output channel.
	return []string{"/bin/sh", "-c", `emit() { echo "$1=$2" >> "$FLINT_OUTPUT"; }; ` + run}, nil
}

// buildEnv assembles the step's non-secret environment. Path-dependent
// variables (FLINT_OUTPUT, FLINT_WORKSPACE) are the runtime's to set — paths
// differ between the host (hostshell) and the container mount layout.
func buildEnv(p *agentv1.StepPayload, step *pipeline.Step) ([]string, error) {
	env := make([]string, 0, len(p.GetEnv())+12)
	for k, v := range p.GetEnv() {
		env = append(env, k+"="+v)
	}
	env = append(env,
		"FLINT_RUN_ID="+p.GetRunId(),
		"FLINT_STEP_NAME="+p.GetStepName(),
		"FLINT_GIT_REPO="+p.GetRepo(),
		"FLINT_GIT_REF="+p.GetRef(),
		"FLINT_GIT_SHA="+p.GetCommitSha(),
		"FLINT_TRIGGER_TYPE="+p.GetTriggerType(),
		"CI=true",
	)
	if mk := p.GetMatrixKey(); mk != "" {
		env = append(env, "FLINT_MATRIX_KEY="+mk)
	}
	// Group steps: the driver reads its spec and expression context from env.
	if p.GetExecType() == "steps" || len(step.Steps) > 0 {
		specJSON, err := json.Marshal(step.Steps)
		if err != nil {
			return nil, fmt.Errorf("marshal sub-step spec: %w", err)
		}
		env = append(env, "FLINT_STEPS_SPEC="+string(specJSON))
		if len(step.DeclaredOutputs) > 0 {
			outJSON, _ := json.Marshal(step.DeclaredOutputs)
			env = append(env, "FLINT_JOB_OUTPUTS="+string(outJSON))
		}
		if needs := p.GetNeedsOutputsJson(); len(needs) > 0 {
			env = append(env, "FLINT_NEEDS_OUTPUTS="+string(needs))
		}
	}
	return env, nil
}

// readEmits parses KEY=VALUE lines from the emit file.
func readEmits(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if k, v, ok := strings.Cut(line, "="); ok && k != "" {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func failResult(msg string) *agentv1.StepResult {
	return &agentv1.StepResult{Status: "failed", ExitCode: 1, ErrorMessage: msg}
}

// serviceSpecs projects the step's services: block into the runtime contract
// (env map → KEY=VALUE).
func serviceSpecs(services []pipeline.Service) []agentruntime.ServiceSpec {
	out := make([]agentruntime.ServiceSpec, 0, len(services))
	for _, s := range services {
		env := make([]string, 0, len(s.Env))
		for k, v := range s.Env {
			env = append(env, k+"="+v)
		}
		out = append(out, agentruntime.ServiceSpec{Name: s.Name, Image: s.Image, Env: env})
	}
	return out
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
