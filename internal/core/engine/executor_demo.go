package engine

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/NerdMeNot/flint/pkg/logsink"
	"github.com/rs/zerolog/log"
)

// LogEmitter lets an in-process executor stream synthetic log lines through the
// same sink + broadcast path the agent ingestion uses, so the existing log SSE
// and log storage work unchanged. Implemented by the server (demo mode).
type LogEmitter interface {
	Emit(ctx context.Context, ref logsink.LogRef, lines []logsink.LogLine)
}

// demoExecutor simulates step execution without Kubernetes — for demo mode only.
// It waits a short randomized time, streams synthetic logs through the LogEmitter,
// occasionally reports failure (so the seeded runs show failures, and steps with
// retry configured retry then often succeed), and reports completion via the
// shared CompleteFunc — exactly the path the http executor uses. Gates never reach
// an executor (dispatchStep short-circuits them), so they pause as real gates.
type demoExecutor struct {
	complete CompleteFunc
	emit     LogEmitter
	failRate int // percent chance an attempt fails
}

// NewDemoExecutor builds the fake executor. complete reports terminal results to
// the engine (use PgEngine.CompleteStep); emit streams synthetic logs.
func NewDemoExecutor(complete CompleteFunc, emit LogEmitter) *demoExecutor {
	return &demoExecutor{complete: complete, emit: emit, failRate: 16}
}

func (e *demoExecutor) Kind() string { return "demo" }

// Dispatch starts the simulated step in a goroutine (fire-and-forget, like the
// http executor) and returns a correlation handle.
func (e *demoExecutor) Dispatch(ctx context.Context, step claimedStep) (string, error) {
	go e.run(step)
	return step.runID + "/" + step.name, nil
}

func (e *demoExecutor) run(step claimedStep) {
	ctx := context.Background()
	ref := logsink.LogRef{OrgID: step.orgID, RunID: step.runID, StepName: step.name}

	lines := 4 + rand.IntN(6)
	totalMs := 2000 + rand.IntN(6000)
	slice := time.Duration(totalMs/(lines+1)) * time.Millisecond

	e.line(ctx, ref, "stdout", fmt.Sprintf("$ flint run %s", step.name))
	for i := 0; i < lines; i++ {
		time.Sleep(slice)
		e.line(ctx, ref, "stdout", fmt.Sprintf("[%s] step %d/%d working…", step.name, i+1, lines))
	}

	// A fresh random draw per attempt: a retried step (max_attempts>1, handled
	// inside CompleteStep) gets independent chances and usually goes green.
	fail := rand.IntN(100) < e.failRate
	res := StepResult{StepName: step.name, Success: !fail}
	if fail {
		res.ExitCode = 1
		res.Error = "simulated failure (demo)"
		e.line(ctx, ref, "stderr", "Error: simulated failure (demo)")
	} else {
		e.line(ctx, ref, "stdout", "✓ done")
	}

	if e.complete != nil {
		if err := e.complete(ctx, step.taskToken, res); err != nil {
			log.Error().Err(err).Str("step", step.name).Msg("engine: demo completion callback failed")
		}
	}
}

func (e *demoExecutor) line(ctx context.Context, ref logsink.LogRef, stream, content string) {
	if e.emit == nil {
		return
	}
	e.emit.Emit(ctx, ref, []logsink.LogLine{{Timestamp: time.Now(), Stream: stream, Content: content}})
}
