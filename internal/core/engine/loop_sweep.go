package engine

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// cleanupFinishedRuns tears down executor resources (outstanding step
// assignments) for terminal runs not yet cleaned. Exactly-once via
// pipeline_runs.cleaned_at; runs every tick so cleanup is prompt for ALL
// terminal transitions — cancel, fail, succeed — not just the sweep window.
// Marks runs cleaned even when no executor needs cleanup (e.g. http-only or
// DB-only mode) so the pending-cleanup index stays bounded.
func (l *Loop) cleanupFinishedRuns(ctx context.Context) {
	q := db.New(l.pool)
	runIDs, err := q.RunsNeedingCleanup(ctx, 100)
	if err != nil {
		log.Warn().Err(err).Msg("engine: list runs needing cleanup failed")
		return
	}
	cleaners := l.executors.cleaners()
	for _, runID := range runIDs {
		for _, c := range cleaners {
			_ = c.CleanupRun(ctx, runID)
		}
		if err := q.MarkRunCleaned(ctx, runID); err != nil {
			log.Warn().Err(err).Str("run", runID).Msg("engine: mark run cleaned failed")
		}
	}
}

// finishStaleWorkflows recovers workflows stuck 'running' with every step
// terminal — the finish was lost to a worker crash. It routes each through the
// real finishWorkflow path (finish the workflow, emit the workflow_finished
// event, FinishRun, enqueue webhooks) with the verdict derived from the steps,
// rather than the old bare UPDATE that stranded the run 'running' with no
// webhook and always recorded 'failed'. Claim and finish share one transaction
// so the FOR UPDATE ... SKIP LOCKED locks held by the claim protect the finish;
// a workflow a live advance is already finishing is skipped (locked) this tick.
func (l *Loop) finishStaleWorkflows(ctx context.Context) {
	tx, err := l.pool.Begin(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("engine: begin tx for stale-workflow finish")
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(l.pool).WithTx(tx)

	stale, err := qtx.ClaimStaleWorkflows(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("engine: claim stale workflows failed")
		return
	}
	if len(stale) == 0 {
		return
	}
	for _, w := range stale {
		status := "succeeded"
		if w.HasFailure {
			status = "failed"
		}
		log.Warn().Str("workflow", w.ID).Str("status", status).
			Msg("engine: sweep recovered stale workflow (finishing)")
		finishWorkflow(ctx, qtx, w.ID, status)
	}
	if err := tx.Commit(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: commit stale-workflow finish failed")
		_ = tx.Rollback(ctx)
		return
	}
	// Wake any waiting parents/webhook processing for the recovered workflows.
	q := db.New(l.pool)
	for _, w := range stale {
		_ = q.NotifyEngine(ctx, w.ID)
		l.engine.notifyState(ctx, w.ID)
	}
}

// sweep detects and recovers from stale state.
func (l *Loop) sweep(ctx context.Context) {
	log.Debug().Msg("engine: sweep started")

	q := db.New(l.pool)

	// 1. Recover steps stuck mid-lifecycle, then advance any workflow that just had
	//    a step fail so the failure propagates (run onFailure, skip downstream,
	//    finish) instead of wedging.
	failed := false

	// 1a. Running steps past their execution deadline.
	if count, err := q.SweepStaleRunningSteps(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: sweep stale running steps failed")
	} else if count > 0 {
		log.Warn().Int64("count", count).Msg("engine: sweep recovered stale steps")
		failed = true
	}

	// 1b. Backstop: gate/wait steps still 'waiting' although their timeout timer
	//     already fired (should never happen with atomic timer handling).
	if count, err := q.FailOrphanedWaitingSteps(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: sweep orphaned waiting steps failed")
	} else if count > 0 {
		log.Warn().Int64("count", count).Msg("engine: sweep failed orphaned waiting steps")
		failed = true
	}

	if failed {
		l.advanceRecentlyFailed(ctx, q)
	}

	// 1c. Steps claimed (running) but never dispatched — the worker died between
	//     claiming and creating the Job. Re-queue them (they never executed). Only
	//     meaningful when executors are registered (DB-only mode leaves steps
	//     running on purpose).
	if len(l.executors) > 0 {
		if count, err := q.RequeueUndispatchedSteps(ctx, l.config.dispatchGrace().Seconds()); err != nil {
			log.Warn().Err(err).Msg("engine: sweep requeue undispatched steps failed")
		} else if count > 0 {
			log.Warn().Int64("count", count).Msg("engine: sweep re-queued undispatched steps")
		}
	}

	// 2. Stale workflows where all steps are terminal but workflow still "running".
	l.finishStaleWorkflows(ctx)

	// 3. Recover outbox events stranded in 'processing' by a worker crash.
	if count, err := q.RecoverStaleOutboxEvents(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: recover stale outbox events failed")
	} else if count > 0 {
		log.Warn().Int64("count", count).Msg("engine: recovered stranded outbox events")
	}

	// 4. Clean up fired timers older than 1 hour, resolved outbox events (>7d),
	// and consumed signals (>7d) so these tables don't grow unbounded.
	if err := q.CleanupFiredTimers(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: cleanup fired timers failed")
	}
	if err := q.CleanResolvedOutbox(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: cleanup resolved outbox failed")
	}
	if err := q.DeleteConsumedSignals(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: cleanup consumed signals failed")
	}
	if err := q.DeleteStaleUnconsumedSignals(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: cleanup stale unconsumed signals failed")
	}
	if err := q.CleanupOldEngineEvents(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: cleanup old engine events failed")
	}
	// Runs retention: terminal runs past the window are deleted (workflows/
	// steps cascade). Batched — one batch per sweep keeps the delete bounded;
	// a backlog drains across sweeps.
	if days := l.config.runRetentionDays(); days > 0 {
		if n, err := q.DeleteOldRuns(ctx, int32(days)); err != nil {
			log.Warn().Err(err).Msg("engine: runs retention failed")
		} else if n > 0 {
			log.Info().Int64("count", n).Int("retentionDays", days).Msg("engine: runs retention pruned old runs")
		}
		// step_assignments do NOT cascade from runs (no FK on the hot dispatch
		// path), so prune the terminal ones on the same window — they're the
		// fattest unbounded table.
		if err := q.CleanupOldStepAssignments(ctx, int32(days)); err != nil {
			log.Warn().Err(err).Msg("engine: step-assignment retention failed")
		}
	}
	// Fleet append-only tables that grow forever otherwise (the engine sweep runs
	// in every worker, so this covers dispatch-only deployments too).
	if err := q.CleanupOldMachineEvents(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: machine-events retention failed")
	}
	if err := q.CleanupOldFleetDecisions(ctx); err != nil {
		log.Warn().Err(err).Msg("engine: fleet-decisions retention failed")
	}

	// 5. Tear down resources for terminal runs not yet cleaned. Same exactly-once
	// path as the per-tick cleanup; kept here as a backstop in case a run reached
	// a terminal state without a following tick.
	l.cleanupFinishedRuns(ctx)

	log.Debug().Msg("engine: sweep completed")
}

// advanceRecentlyFailed advances every workflow that had a step fail in the last
// few seconds, so a sweep-induced failure propagates to downstream steps. Each
// advance runs in its own transaction.
func (l *Loop) advanceRecentlyFailed(ctx context.Context, q *db.Queries) {
	wfIDs, err := q.RecentlyFailedWorkflowIDs(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("engine: sweep failed to list recently failed workflows")
		return
	}
	for _, wfID := range wfIDs {
		tx, txErr := l.pool.Begin(ctx)
		if txErr != nil {
			log.Warn().Err(txErr).Msg("engine: sweep failed to begin tx for advancement")
			continue
		}
		qtx := db.New(l.pool).WithTx(tx)
		if err := advanceWorkflow(ctx, qtx, wfID, 0); err != nil {
			log.Warn().Err(err).Str("workflow", wfID).Msg("engine: sweep advancement failed")
		}
		if err := tx.Commit(ctx); err != nil {
			_ = tx.Rollback(ctx)
		}
	}
}

// runOutbox delivers webhook outbox events on its own goroutine, decoupled from
// the tick loop. NOTIFY wakes it promptly (a finished/cancelled run enqueues
// webhook events and signals the engine channel); polling is only the fallback,
// so it follows the same adaptive cadence as the main loop.
func (l *Loop) runOutbox(ctx context.Context) {
	timer := time.NewTimer(l.config.pollInterval())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-l.outboxWake:
			processOutbox(ctx, l.pool)
		case <-timer.C:
			processOutbox(ctx, l.pool)
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		if l.notifyHealthy.Load() {
			timer.Reset(l.config.idlePollInterval())
		} else {
			timer.Reset(l.config.pollInterval())
		}
	}
}
