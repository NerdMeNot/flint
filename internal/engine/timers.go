package engine

import (
	"context"

	"github.com/NerdMeNot/flint/internal/db"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
)

// fireTimers claims and processes all due timers.
func fireTimers(ctx context.Context, pool *pgxpool.Pool) error {
	q := db.New(pool)
	timers, err := q.FireDueTimers(ctx)
	if err != nil {
		return err
	}
	for _, t := range timers {
		if err := handleFiredTimer(ctx, pool, t); err != nil {
			log.Error().Err(err).
				Str("timer", t.TimerType).
				Str("step", t.StepName).
				Msg("engine: failed to handle timer")
		}
	}
	return nil
}

func handleFiredTimer(ctx context.Context, pool *pgxpool.Pool, t db.FireDueTimersRow) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	qtx := db.New(pool).WithTx(tx)

	switch t.TimerType {
	case "timeout":
		// Step execution timed out.
		err = qtx.FailStepByTimeout(ctx, db.FailStepByTimeoutParams{
			Result:     mustJSON(StepResult{StepName: t.StepName, Success: false, Error: "step timed out"}),
			WorkflowID: t.WorkflowID,
			StepName:   t.StepName,
		})
		log.Warn().Str("step", t.StepName).Msg("engine: step timed out")

	case "gate_timeout":
		// Gate approval timed out.
		err = qtx.FailGateByTimeout(ctx, db.FailGateByTimeoutParams{
			Result:     mustJSON(StepResult{StepName: t.StepName, Success: false, Error: "gate approval timed out"}),
			WorkflowID: t.WorkflowID,
			StepName:   t.StepName,
		})
		log.Warn().Str("step", t.StepName).Msg("engine: gate timed out")

	case "watch_timeout":
		// Watch condition timed out.
		err = qtx.FailGateByTimeout(ctx, db.FailGateByTimeoutParams{
			Result:     mustJSON(StepResult{StepName: t.StepName, Success: false, Error: "watch condition timed out"}),
			WorkflowID: t.WorkflowID,
			StepName:   t.StepName,
		})
		log.Warn().Str("step", t.StepName).Msg("engine: watch timed out")

	case "watch_interval":
		// Time to check the watch condition again.
		// For now, we just log — the actual HTTP/K8s check needs to be implemented.
		log.Debug().Str("step", t.StepName).Msg("engine: watch interval fired (check not implemented)")

		// Re-create interval timer for next check.
		_ = qtx.UpsertTimer(ctx, db.UpsertTimerParams{
			WorkflowID: t.WorkflowID,
			StepName:   t.StepName,
			TimerType:  "watch_interval",
			Secs:       15,
		})
		return tx.Commit(ctx) // Don't advance — watch is still waiting.

	case "retry_backoff":
		// Retry backoff expired — re-queue the step.
		_ = qtx.RequeueRetryStep(ctx, db.RequeueRetryStepParams{
			WorkflowID: t.WorkflowID,
			Name:       t.StepName,
		})
		log.Info().Str("step", t.StepName).Msg("engine: retry backoff expired, re-queuing")
		return tx.Commit(ctx) // Don't advance — step just got re-queued.
	}

	if err != nil {
		return err
	}

	// Advance workflow after timeout/failure.
	if err := advanceWorkflow(ctx, qtx, t.WorkflowID, 0); err != nil {
		return err
	}

	return tx.Commit(ctx)
}
