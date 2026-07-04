package fleet

import (
	"context"
	"time"

	"github.com/rs/zerolog/log"
)

// LoopConfig tunes the fleet loop's cadences.
type LoopConfig struct {
	// TickInterval drives scheduling and lease sweeps (default 1s — binding
	// latency is part of queue-to-start time).
	TickInterval time.Duration
}

// Loop is the fleet manager's control loop: every tick it expires deadlines,
// releases unclaimed work, schedules pending assignments, provisions for
// deficit, and scales down idle machines; provider reconciliation runs on a
// slower cadence. It mirrors the engine loop's shape and runs beside it in
// the dispatch process.
type Loop struct {
	fleet *Fleet
	cfg   LoopConfig
	ticks int
}

// NewLoop builds the fleet loop.
func NewLoop(f *Fleet, cfg LoopConfig) *Loop {
	if cfg.TickInterval <= 0 {
		cfg.TickInterval = time.Second
	}
	return &Loop{fleet: f, cfg: cfg}
}

// Run blocks until ctx is done.
func (l *Loop) Run(ctx context.Context) error {
	log.Info().Dur("tick", l.cfg.TickInterval).Msg("fleet: loop running")
	t := time.NewTicker(l.cfg.TickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			l.tick(ctx)
		}
	}
}

// reconcileEveryTicks paces provider List calls (~30s at the default tick).
const reconcileEveryTicks = 30

func (l *Loop) tick(ctx context.Context) {
	l.ticks++
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
	// Provision AFTER scheduling: what remains pending is genuinely
	// unschedulable demand.
	if _, err := l.fleet.Provision(ctx); err != nil {
		log.Error().Err(err).Msg("fleet: provisioning failed")
	}
	if _, err := l.fleet.ScaleDown(ctx); err != nil {
		log.Error().Err(err).Msg("fleet: scale-down failed")
	}
	if l.ticks%reconcileEveryTicks == 0 {
		if err := l.fleet.Reconcile(ctx); err != nil {
			log.Error().Err(err).Msg("fleet: reconcile failed")
		}
	}
}
