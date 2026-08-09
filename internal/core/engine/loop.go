package engine

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// Loop is the worker main loop. It polls Postgres for queued steps,
// fires timers, dispatches steps via a StepExecutor, and sweeps for stale state.
type Loop struct {
	pool       db.Pool
	engine     *PgEngine
	executors  ExecutorRegistry
	config     LoopConfig
	wake       chan struct{}
	outboxWake chan struct{}
	// notifyHealthy tracks whether the LISTEN connection is live. When it is,
	// NOTIFY wakeups are the fast path and polling backs off to the idle
	// interval; when it drops, polling falls back to the tight interval.
	notifyHealthy atomic.Bool
}

// NewLoop creates a worker loop. executors maps step exec types to the executor
// that runs them; an empty registry runs the loop in DB-only mode (steps are
// claimed but never dispatched).
func NewLoop(engine *PgEngine, executors ExecutorRegistry, cfg LoopConfig) *Loop {
	return &Loop{
		pool:       engine.pool,
		engine:     engine,
		executors:  executors,
		config:     cfg,
		wake:       make(chan struct{}, 1),
		outboxWake: make(chan struct{}, 1),
	}
}

// Run starts the main loop. Blocks until context cancellation.
func (l *Loop) Run(ctx context.Context) error {
	log.Info().
		Dur("pollInterval", l.config.pollInterval()).
		Dur("idlePollInterval", l.config.idlePollInterval()).
		Dur("sweepInterval", l.config.sweepInterval()).
		Msg("engine: worker loop started")

	// Start LISTEN/NOTIFY listener for instant wakeup.
	go l.listenNotify(ctx)

	// Deliver outbox events (webhooks) on a dedicated goroutine so a slow endpoint
	// can never stall step claiming/dispatch on the tick loop.
	go l.runOutbox(ctx)

	// Adaptive polling: while LISTEN/NOTIFY is healthy, notifications carry the
	// work signal and the poll is only a safety net (timers still need it) —
	// back off to the idle interval. Without a healthy listener, poll tight.
	timer := time.NewTimer(l.config.pollInterval())
	sweepTicker := time.NewTicker(l.config.sweepInterval())
	defer timer.Stop()
	defer sweepTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("engine: worker loop stopped")
			return nil
		case <-l.wake:
			l.tick(ctx)
		case <-timer.C:
			l.tick(ctx)
		case <-sweepTicker.C:
			l.sweep(ctx)
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(l.currentPollInterval())
	}
}

// currentPollInterval returns the poll cadence for the current NOTIFY health.
// Timers cap the backoff: a due timer must not wait for a 30s poll, so when
// one fires sooner than the idle interval the wait shrinks to it.
func (l *Loop) currentPollInterval() time.Duration {
	if !l.notifyHealthy.Load() {
		return l.config.pollInterval()
	}
	idle := l.config.idlePollInterval()
	if due, err := db.New(l.pool).NextTimerDue(context.Background()); err == nil && !due.IsZero() {
		if wait := time.Until(due); wait < idle {
			if wait < l.config.pollInterval() {
				return l.config.pollInterval()
			}
			return wait
		}
	}
	return idle
}

// tick is one iteration of the main loop.
func (l *Loop) tick(ctx context.Context) {
	// Phase 1: Fire due timers.
	if err := fireTimers(ctx, l.pool); err != nil {
		log.Error().Err(err).Msg("engine: fire timers error")
	}

	// Phase 2: Process gate approval/rejection signals and external-signal waits.
	l.processSignals(ctx)
	l.processRejections(ctx)
	l.processSignalWaits(ctx)

	// Phase 2.5: Advance workflows with pending step-result signals.
	// This is the crash-recovery path: when an agent dies without reporting,
	// the fleet's machine-lost sweep delivers a step-result signal within
	// seconds — without this phase nothing would consume it until the step's
	// timeout sweep (hours later), because tick otherwise only advances via
	// completions.
	l.processStepResultSignals(ctx)

	// Phase 3: Claim and dispatch queued steps.
	l.claimAndDispatch(ctx)

	// Phase 4: Tear down resources for runs that just reached a terminal state.
	l.cleanupFinishedRuns(ctx)
}

// listenNotify listens for Postgres NOTIFY events for instant wakeup.
// Requires the underlying pool to be *pgxpool.Pool (for Acquire). If the pool
// implementation doesn't support Acquire, falls back to polling-only mode.
func (l *Loop) listenNotify(ctx context.Context) {
	pgPool, ok := l.pool.(*pgxpool.Pool)
	if !ok {
		log.Warn().Msg("engine: pool does not support Acquire, LISTEN/NOTIFY disabled (polling-only mode)")
		return
	}

	for {
		conn, err := pgPool.Acquire(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			time.Sleep(time.Second)
			continue
		}

		_, err = conn.Exec(ctx, "LISTEN flint_engine")
		if err != nil {
			conn.Release()
			time.Sleep(time.Second)
			continue
		}

		// Listener live — polling may back off to the idle cadence.
		l.notifyHealthy.Store(true)

		for {
			_, err := conn.Conn().WaitForNotification(ctx)
			if err != nil {
				// Listener down — fall back to tight polling until reconnected.
				l.notifyHealthy.Store(false)
				conn.Release()
				if ctx.Err() != nil {
					return
				}
				break // reconnect
			}
			// Wake both the tick loop (advancement) and the outbox delivery loop:
			// a committed state transition both queues work and may have enqueued
			// webhook events. Non-blocking — a coalesced wake is fine.
			select {
			case l.wake <- struct{}{}:
			default:
			}
			select {
			case l.outboxWake <- struct{}{}:
			default:
			}
		}
	}
}
