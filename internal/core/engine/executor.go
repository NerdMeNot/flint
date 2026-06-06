package engine

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"
)

// StepExecutor starts execution of a claimed step. Implementations are
// fire-and-forget: step completion is reported out-of-band via the
// /internal/complete callback (the k8s executor also has the informer fallback).
//
// This is the seam that lets the engine run work in different ways — a
// Kubernetes Job, a local process, an HTTP call — without the loop or the rest
// of the engine knowing which.
type StepExecutor interface {
	// Kind identifies the executor ("k8s", "local", …) for logs and metrics.
	Kind() string
	// Dispatch starts the step and returns an opaque handle for correlation
	// (e.g. the k8s Job name), or an error if it could not be started. An empty
	// handle is valid (nothing to correlate).
	Dispatch(ctx context.Context, step claimedStep) (handle string, err error)
}

// stepCleaner is an optional StepExecutor capability: release any resources
// associated with a finished run (e.g. a k8s workspace pod and leftover Jobs).
// Executors with nothing to clean up (e.g. local processes) simply don't
// implement it, and the sweep skips cleanup for them.
type stepCleaner interface {
	CleanupRun(ctx context.Context, runID string) error
}

// dispatchStep routes a claimed step to the executor by exec type. Gates don't
// execute — they wait for an approval signal (and are claimed as "waiting", so
// they don't reach here in practice); unknown types are an error.
func dispatchStep(ctx context.Context, exec StepExecutor, step claimedStep) (string, error) {
	switch step.execType {
	case "run", "use", "steps":
		return exec.Dispatch(ctx, step)
	case "gate":
		log.Info().Str("step", step.name).Msg("engine: gate step waiting for approval")
		return "", nil
	default:
		return "", fmt.Errorf("engine: unknown step type %q", step.execType)
	}
}
