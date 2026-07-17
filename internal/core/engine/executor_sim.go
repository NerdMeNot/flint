package engine

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/NerdMeNot/flint/pkg/logsink"
	"github.com/rs/zerolog/log"
)

// simExecutor runs container-type steps (run/use/steps) WITHOUT a Pod, container,
// or agent — it sleeps, writes synthetic logs, and reports a scripted outcome via
// the shared CompleteFunc. It is a test double for local development and demos: it
// runs NO user code (it is not the retired local/docker executor). It lets the
// REAL engine — advance/dispatch loop, timers, gates, outbox, SSE — drive runs to
// completion against a real database without a Kubernetes cluster.
//
// Outcomes are scripted per job via env vars (carried from the job's `env:` block,
// which survives compile into claimedStep.env):
//
//	env:
//	  SIM_DURATION: "2s"      # how long the job "runs" (default 1s)
//	  SIM_OUTCOME: "fail"     # "succeed" (default) or "fail"
//	  SIM_FLAKY_UNTIL: "2"    # fail until this many dispatches, then succeed
//
// A failed job makes the engine skip its downstream jobs — a real failure cascade,
// not a faked one. SIM_FLAKY_UNTIL exercises the engine's retry path when the
// step carries a retry spec.
type simExecutor struct {
	complete CompleteFunc
	sink     logsink.LogSink

	mu         sync.Mutex
	dispatches map[string]int // run/step key → times dispatched (for flaky-then-pass)
}

// NewSimExecutor builds the simulated step executor. sink may be nil (logs are
// then skipped); completion is reported via complete, mirroring the http executor.
func NewSimExecutor(complete CompleteFunc, sink logsink.LogSink) *simExecutor {
	return &simExecutor{complete: complete, sink: sink, dispatches: make(map[string]int)}
}

func (e *simExecutor) Kind() string { return "sim" }

// Dispatch records the dispatch (for flaky steps) and runs the simulation in a
// goroutine. The handle is the run/step identity.
func (e *simExecutor) Dispatch(_ context.Context, step claimedStep) (string, error) {
	key := step.runID + "/" + step.name
	e.mu.Lock()
	e.dispatches[key]++
	attempt := e.dispatches[key]
	e.mu.Unlock()

	go e.run(step, attempt)
	return key, nil
}

func (e *simExecutor) run(step claimedStep, attempt int) {
	ctx := context.Background()

	dur := simDuration(step.env["SIM_DURATION"])
	ref := logsink.LogRef{OrgID: step.orgID, RunID: step.runID, StepName: step.name}

	// Synthetic output shaped like real agent output: grouped sections
	// (`~~~` muted, `--- ` collapsed, `+++ ` expanded — the log view's group
	// markers) with ANSI color, spread across the simulated duration so log
	// streaming and per-group durations are exercised end to end.
	quarter := dur / 4
	e.write(ctx, ref, "stdout", "~~~ Preparing machine")
	e.write(ctx, ref, "stdout", "[sim] "+step.name+" claimed (attempt "+strconv.Itoa(attempt)+")")
	e.write(ctx, ref, "stdout", "[sim] image ghcr.io/flint/sim:latest \x1b[32mready\x1b[0m")
	time.Sleep(quarter)

	e.write(ctx, ref, "stdout", "--- Restoring cache")
	e.write(ctx, ref, "stdout", "cache key \x1b[90msim-"+step.name+"\x1b[0m")
	e.write(ctx, ref, "stdout", "\x1b[36mrestored 128 MiB in 0.4s\x1b[0m")
	time.Sleep(quarter)

	e.write(ctx, ref, "stdout", "+++ Running "+step.name)
	e.write(ctx, ref, "stdout", "\x1b[90m$ flint sim exec "+step.name+"\x1b[0m")
	time.Sleep(dur - 2*quarter)

	result := simOutcome(step.name, step.env, attempt)
	if result.Success {
		e.write(ctx, ref, "stdout", "\x1b[32m✓\x1b[0m "+step.name+" completed in "+dur.String())
	} else {
		e.write(ctx, ref, "stderr", "\x1b[31m✗ "+step.name+" failed: "+result.Error+"\x1b[0m (exit 1)")
	}

	log.Info().Str("step", step.name).Str("kind", "sim").
		Bool("ok", result.Success).Int("attempt", attempt).Dur("dur", dur).
		Msg("engine: sim step finished")

	if e.complete != nil {
		if err := e.complete(ctx, step.taskToken, result); err != nil {
			log.Error().Err(err).Str("step", step.name).Msg("engine: sim completion callback failed")
		}
	}
}

func (e *simExecutor) write(ctx context.Context, ref logsink.LogRef, stream, content string) {
	if e.sink == nil {
		return
	}
	line := logsink.LogLine{Timestamp: time.Now(), Stream: stream, Content: content}
	if err := e.sink.Write(ctx, ref, []logsink.LogLine{line}); err != nil {
		log.Warn().Err(err).Str("step", ref.StepName).Msg("engine: sim log write failed")
	}
}

// simDuration parses the SIM_DURATION hint, defaulting to 1s.
func simDuration(raw string) time.Duration {
	if raw == "" {
		return time.Second
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return time.Second
	}
	return d
}

// simOutcome decides a job's scripted result from its sim env hints. SIM_FLAKY_UNTIL
// takes precedence (fail until the given attempt), then SIM_OUTCOME.
func simOutcome(stepName string, env map[string]string, attempt int) StepResult {
	if raw := env["SIM_FLAKY_UNTIL"]; raw != "" {
		if until, err := strconv.Atoi(raw); err == nil && attempt < until {
			return StepResult{StepName: stepName, Success: false, ExitCode: 1, Error: "simulated flaky failure"}
		}
		return StepResult{StepName: stepName, Success: true}
	}
	if env["SIM_OUTCOME"] == "fail" {
		return StepResult{StepName: stepName, Success: false, ExitCode: 1, Error: "simulated failure"}
	}
	return StepResult{StepName: stepName, Success: true}
}
