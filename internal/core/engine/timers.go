package engine

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// maxTimersPerTick bounds how many due timers a single tick processes so timer
// handling can't starve the loop's other phases under a backlog.
const maxTimersPerTick = 100

// fireTimers claims and handles due timers one at a time, each in its own
// transaction. Crucially, a timer is marked fired in the SAME transaction as its
// effect (LockNextDueTimer holds the row lock across handling), so a crash or
// error mid-handle rolls back and the timer is retried on the next tick. This is
// exactly-once handling, replacing the previous at-most-once flow where fired=true
// committed before the effect ran (a crash there stranded gate/wait timeouts).
func fireTimers(ctx context.Context, pool db.Pool) error {
	for i := 0; i < maxTimersPerTick; i++ {
		done, err := fireOneTimer(ctx, pool)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	return nil
}

// fireOneTimer claims and handles a single due timer in one transaction. Returns
// done=true when no due timer remains (or a handler error left work for the next
// tick).
func fireOneTimer(ctx context.Context, pool db.Pool) (done bool, err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(pool).WithTx(tx)

	t, err := qtx.LockNextDueTimer(ctx)
	if err != nil {
		if err == pgx.ErrNoRows {
			return true, nil
		}
		return false, err
	}

	if err := handleTimer(ctx, qtx, t); err != nil {
		// Roll back (deferred) and stop this tick; the timer stays fired=false and
		// is retried next tick.
		log.Error().Err(err).Str("timer", t.TimerType).Str("step", t.StepName).
			Msg("engine: failed to handle timer (will retry)")
		return true, nil
	}
	if err := qtx.MarkTimerFired(ctx, t.ID); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return false, nil
}

// handleTimer applies a due timer's effect inside the caller's transaction.
func handleTimer(ctx context.Context, qtx *db.Queries, t db.LockNextDueTimerRow) error {
	switch t.TimerType {
	case timerTimeout:
		// Step execution timed out.
		if err := qtx.FailStepByTimeout(ctx, db.FailStepByTimeoutParams{
			Result:     mustJSON(StepResult{StepName: t.StepName, Success: false, Error: "step timed out"}),
			WorkflowID: t.WorkflowID,
			StepName:   t.StepName,
		}); err != nil {
			return err
		}
		emitStepEvent(ctx, qtx, stepTransition{
			workflowID: t.WorkflowID, stepName: t.StepName, attempt: -1,
			from: stepRunning, to: stepFailed, eventType: "timed_out", actor: actorEngine,
			reason: "step execution timed out",
		})
		log.Warn().Str("step", t.StepName).Msg("engine: step timed out")
		return advanceWorkflow(ctx, qtx, t.WorkflowID, 0)

	case timerGateTimeout, timerWaitTimeout:
		// Gate approval / external-signal wait timed out. FailGateByTimeout
		// transitions any 'waiting' step → failed, which covers both gate and wait.
		msg := "gate approval timed out"
		if t.TimerType == timerWaitTimeout {
			msg = "wait timed out"
		}
		if err := qtx.FailGateByTimeout(ctx, db.FailGateByTimeoutParams{
			Result:     mustJSON(StepResult{StepName: t.StepName, Success: false, Error: msg}),
			WorkflowID: t.WorkflowID,
			StepName:   t.StepName,
		}); err != nil {
			return err
		}
		emitStepEvent(ctx, qtx, stepTransition{
			workflowID: t.WorkflowID, stepName: t.StepName, attempt: -1,
			from: stepWaiting, to: stepFailed, eventType: "timed_out", actor: actorEngine, reason: msg,
		})
		log.Warn().Str("step", t.StepName).Str("type", t.TimerType).Msg("engine: " + msg)
		return advanceWorkflow(ctx, qtx, t.WorkflowID, 0)

	case timerRetryBackoff:
		// Backoff expired — promote the parked retry attempt (retry_wait → queued).
		// Do NOT advance: the step re-enters via the normal claim path.
		if err := qtx.RequeueRetryStep(ctx, db.RequeueRetryStepParams{
			WorkflowID: t.WorkflowID,
			Name:       t.StepName,
		}); err != nil {
			return err
		}
		emitStepEvent(ctx, qtx, stepTransition{
			workflowID: t.WorkflowID, stepName: t.StepName, attempt: -1,
			from: stepRetryWait, to: stepQueued, eventType: "retry_requeued", actor: actorEngine,
		})
		log.Info().Str("step", t.StepName).Msg("engine: retry backoff expired, re-queuing")
		return nil
	}
	return nil
}
