package engine

import (
	"context"
	"errors"

	"github.com/rs/zerolog/log"
)

// errNoExecutor means no executor is registered for a step's exec type. The loop
// treats it as "leave the step running" (e.g. DB-only mode) rather than a
// failure.
var errNoExecutor = errors.New("engine: no executor registered for step type")

// CompleteFunc reports a step's terminal result back to the engine. In-process
// executors (e.g. http) invoke it when a step finishes — standing in for the
// agent's /internal/complete callback that the k8s executor relies on.
type CompleteFunc func(ctx context.Context, taskToken string, result StepResult) error

// StepExecutor starts execution of a claimed step. Implementations are
// fire-and-forget: step completion is reported out-of-band via the
// /internal/complete callback (the k8s executor also has the informer fallback).
//
// This is the seam that lets the engine run work in different ways — a
// Kubernetes Job, an HTTP call — without the loop or the rest of the engine
// knowing which.
type StepExecutor interface {
	// Kind identifies the executor ("k8s", "http", …) for logs and metrics.
	Kind() string
	// Dispatch starts the step and returns an opaque handle for correlation
	// (e.g. the k8s Job name), or an error if it could not be started. An empty
	// handle is valid (nothing to correlate).
	Dispatch(ctx context.Context, step claimedStep) (handle string, err error)
}

// stepCleaner is an optional StepExecutor capability: release any resources
// associated with a finished run (e.g. a k8s workspace pod and leftover Jobs).
// Executors with nothing to clean up (e.g. http) simply don't implement it,
// and the sweep skips cleanup for them.
type stepCleaner interface {
	CleanupRun(ctx context.Context, runID string) error
}

// ExecutorRegistry maps a step's exec type to the executor that runs it (e.g.
// "run"/"use"/"steps" → a container executor, "http" → the http executor). This
// is what makes the engine execution-model-agnostic: products register the
// executors they need.
type ExecutorRegistry map[string]StepExecutor

// cleaners returns the distinct executors in the registry that need per-run
// cleanup (deduped, since one executor may be registered under several types).
func (r ExecutorRegistry) cleaners() []stepCleaner {
	seen := make(map[StepExecutor]bool, len(r))
	var out []stepCleaner
	for _, ex := range r {
		if seen[ex] {
			continue
		}
		seen[ex] = true
		if c, ok := ex.(stepCleaner); ok {
			out = append(out, c)
		}
	}
	return out
}

// dispatchStep routes a claimed step to the registered executor by exec type.
// Gates/approvals don't execute — they wait for a signal (and are claimed as
// "waiting", so they don't reach here in practice). A step type with no
// registered executor returns errNoExecutor, which the loop treats as "leave
// running" rather than a failure.
func dispatchStep(ctx context.Context, registry ExecutorRegistry, step claimedStep) (string, error) {
	switch step.execType {
	case "gate", "approval":
		log.Info().Str("step", step.name).Msg("engine: gate step waiting for approval")
		return "", nil
	}
	exec, ok := registry[step.execType]
	if !ok {
		return "", errNoExecutor
	}
	return exec.Dispatch(ctx, step)
}
