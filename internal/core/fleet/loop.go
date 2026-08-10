package fleet

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// LoopConfig tunes the fleet loop's cadences. Zero values take the defaults
// described on each field.
type LoopConfig struct {
	// TickInterval drives the fast loop — deadline sweeps, unclaimed release,
	// and assignment scheduling (default 1s). All pure Postgres work; binding
	// latency is part of queue-to-start time, so this stays tight.
	TickInterval time.Duration

	// CapacityInterval drives the capacity loop — provisioning and scale-down
	// (default: TickInterval). These make blocking provider API calls, so they
	// run on their own goroutine; the interval is the gap between passes, not a
	// budget for one.
	CapacityInterval time.Duration

	// ReconcileInterval paces provider reconciliation, which lists a provider's
	// entire inventory (default: 30 × TickInterval).
	ReconcileInterval time.Duration
}

func (c LoopConfig) withDefaults() LoopConfig {
	if c.TickInterval <= 0 {
		c.TickInterval = time.Second
	}
	if c.CapacityInterval <= 0 {
		c.CapacityInterval = c.TickInterval
	}
	if c.ReconcileInterval <= 0 {
		c.ReconcileInterval = 30 * c.TickInterval
	}
	return c
}

// Loop is the fleet manager's control loop. It runs as THREE independent
// goroutines, split by what they block on:
//
//	fast      — deadline sweeps, unclaimed release, scheduling. Pure Postgres,
//	            sub-millisecond, latency-critical.
//	capacity  — provisioning and scale-down. Blocks on provider Quote/Create/
//	            Destroy.
//	reconcile — provider inventory reconciliation. Blocks on provider List.
//
// The split is not an optimisation, it is a correctness property. Sharing one
// goroutine meant a slow (or hung) cloud API call stalled heartbeat expiry and
// scheduling for the whole fleet: machines could die unnoticed because the loop
// was parked inside a TCP read. Provider calls are separately bounded by
// ProviderTimeouts, so a hang is now both isolated AND finite.
//
// Fleet itself is safe for concurrent use — all shared state is in Postgres,
// guarded by per-pool advisory locks and optimistic status transitions — so the
// three goroutines need no coordination beyond that.
type Loop struct {
	fleet *Fleet
	cfg   LoopConfig
}

// NewLoop builds the fleet loop.
func NewLoop(f *Fleet, cfg LoopConfig) *Loop {
	return &Loop{fleet: f, cfg: cfg.withDefaults()}
}

// Run blocks until ctx is done, then waits for the three loops to unwind.
func (l *Loop) Run(ctx context.Context) error {
	log.Info().
		Dur("tick", l.cfg.TickInterval).
		Dur("capacity", l.cfg.CapacityInterval).
		Dur("reconcile", l.cfg.ReconcileInterval).
		Msg("fleet: loop running")

	// Inventory gauges live for exactly as long as the loop does, so a stopped
	// fleet stops reporting a fleet rather than freezing its last shape forever.
	if unregister, err := l.fleet.RegisterInventoryMetrics(); err != nil {
		log.Error().Err(err).Msg("fleet: inventory metrics unavailable")
	} else {
		defer func() {
			if err := unregister(); err != nil {
				log.Error().Err(err).Msg("fleet: unregistering inventory metrics failed")
			}
		}()
	}

	loops := []struct {
		interval time.Duration
		pass     func(context.Context)
	}{
		{l.cfg.TickInterval, l.fastPass},
		{l.cfg.CapacityInterval, l.capacityPass},
		{l.cfg.ReconcileInterval, l.reconcilePass},
	}
	var wg sync.WaitGroup
	for _, sub := range loops {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runEvery(ctx, sub.interval, sub.pass)
		}()
	}
	wg.Wait()
	return nil
}

// runEvery calls pass on a fixed cadence until ctx is done. A pass that
// overruns its interval simply delays the next one (time.Ticker drops the
// missed beats) — passes never overlap, so each loop's own work stays
// serialised even when a provider is slow.
func runEvery(ctx context.Context, interval time.Duration, pass func(context.Context)) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pass(ctx)
		}
	}
}

// fastPass is the latency-critical half: expire what has lapsed, then place
// what is pending. Nothing here touches a provider.
func (l *Loop) fastPass(ctx context.Context) {
	if _, err := l.fleet.ExpireBootDeadlines(ctx); err != nil {
		log.Error().Err(err).Msg("fleet: boot-deadline sweep failed")
	}
	if _, err := l.fleet.ExpireHeartbeatLeases(ctx); err != nil {
		log.Error().Err(err).Msg("fleet: heartbeat sweep failed")
	}
	if _, err := l.fleet.ReleaseUnclaimed(ctx); err != nil {
		log.Error().Err(err).Msg("fleet: unclaimed release failed")
	}
	if _, err := l.fleet.SchedulePending(ctx); err != nil {
		log.Error().Err(err).Msg("fleet: scheduling failed")
	}
}

// capacityPass buys and sells capacity. It opens with its OWN scheduling pass:
// provisioning demand is "pending work the scheduler could not place", so it is
// only meaningful after a scheduling pass has run against the same state.
// Reading it on a separate goroutine from the fast loop would otherwise count
// work that is about to land on an idle machine, and boot for it. SchedulePending
// is pure DB and per-pool advisory-locked, so running it on both loops is cheap
// and safe — the second pass finds nothing left to bind.
func (l *Loop) capacityPass(ctx context.Context) {
	if _, err := l.fleet.SchedulePending(ctx); err != nil {
		log.Error().Err(err).Msg("fleet: scheduling failed")
		// Provisioning off an unscheduled view over-provisions. Skip this pass.
		return
	}
	if _, err := l.fleet.Provision(ctx); err != nil {
		log.Error().Err(err).Msg("fleet: provisioning failed")
	}
	if _, err := l.fleet.ScaleDown(ctx); err != nil {
		log.Error().Err(err).Msg("fleet: scale-down failed")
	}
	// Completes drains. Runs beside scale-down because it is the same kind of
	// decision — capacity leaving the fleet — and shares its provider I/O.
	if _, err := l.fleet.TerminateDrained(ctx); err != nil {
		log.Error().Err(err).Msg("fleet: terminating drained machines failed")
	}
}

func (l *Loop) reconcilePass(ctx context.Context) {
	if err := l.fleet.Reconcile(ctx); err != nil {
		log.Error().Err(err).Msg("fleet: reconcile failed")
	}
}
