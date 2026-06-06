package engine

import (
	"context"

	"github.com/NerdMeNot/flint/internal/db"
	"github.com/rs/zerolog/log"
)

// fireTimers claims and processes all due timers.
func fireTimers(ctx context.Context, pool db.Pool) error {
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

func handleFiredTimer(ctx context.Context, pool db.Pool, t db.FireDueTimersRow) error {
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

	case "retry_backoff":
		// Retry backoff expired — re-queue the step.
		if reqErr := qtx.RequeueRetryStep(ctx, db.RequeueRetryStepParams{
			WorkflowID: t.WorkflowID,
			Name:       t.StepName,
		}); reqErr != nil {
			log.Error().Err(reqErr).Str("step", t.StepName).Msg("engine: failed to requeue retry step")
			return reqErr
		}
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
