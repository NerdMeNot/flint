package workflows

import (
	"context"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/google/uuid"
	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog/log"
)

// Scheduler fires cron-scheduled workflow runs. It runs in the worker process
// and is durable via the workflow_schedules table's next_run_at — a missed tick
// (restart) just fires on the next poll. Multi-worker safe: the claim is an
// atomic conditional UPDATE, so only one worker fires a given due schedule.
type Scheduler struct {
	q        db.Querier
	engine   engine.Engine
	interval time.Duration
}

// NewScheduler builds the workflow scheduler.
func NewScheduler(q db.Querier, eng engine.Engine) *Scheduler {
	return &Scheduler{q: q, engine: eng, interval: 30 * time.Second}
}

// Run polls for due schedules until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	log.Info().Dur("interval", s.interval).Msg("workflows: scheduler started")
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

func (s *Scheduler) tick(ctx context.Context) {
	due, err := s.q.ListDueWorkflowSchedules(ctx)
	if err != nil {
		log.Error().Err(err).Msg("workflows: list due schedules failed")
		return
	}
	for _, sch := range due {
		s.fire(ctx, sch)
	}
}

// fire claims and runs a single due schedule.
func (s *Scheduler) fire(ctx context.Context, sch db.ListDueWorkflowSchedulesRow) {
	next, err := NextCron(sch.Cron, time.Now())
	if err != nil {
		log.Error().Err(err).Str("schedule", sch.ID).Str("cron", sch.Cron).Msg("workflows: invalid cron, skipping")
		return
	}

	// Atomic claim: advancing next_run_at returns 1 row only for the winner.
	rows, err := s.q.AdvanceWorkflowScheduleIfDue(ctx, db.AdvanceWorkflowScheduleIfDueParams{
		ID: sch.ID, NextRunAt: next,
	})
	if err != nil {
		log.Error().Err(err).Str("schedule", sch.ID).Msg("workflows: advance schedule failed")
		return
	}
	if rows == 0 {
		return // another worker already claimed this fire
	}

	def, err := Parse([]byte(sch.Definition))
	if err != nil {
		log.Error().Err(err).Str("schedule", sch.ID).Msg("workflows: schedule has invalid definition")
		return
	}
	waves, err := def.Resolve()
	if err != nil {
		log.Error().Err(err).Str("schedule", sch.ID).Msg("workflows: schedule DAG resolve failed")
		return
	}

	runID := uuid.NewString()
	triggeredBy := "scheduler"
	if err := s.q.InsertWorkflowRun(ctx, db.InsertWorkflowRunParams{
		ID: runID, OrgID: sch.OrgID, TriggerType: "schedule", TriggeredBy: &triggeredBy,
	}); err != nil {
		log.Error().Err(err).Str("schedule", sch.ID).Msg("workflows: create scheduled run failed")
		return
	}
	if _, err := s.engine.StartWorkflowWithWaves(ctx, engine.StartWorkflowInput{
		RunID: runID, OrgID: sch.OrgID, Kind: "workflow",
	}, waves); err != nil {
		log.Error().Err(err).Str("schedule", sch.ID).Msg("workflows: start scheduled workflow failed")
		return
	}
	log.Info().Str("schedule", sch.ID).Str("run", runID).Time("next", next).
		Msg("workflows: scheduled run started")
}

// NextCron computes the next fire time for a standard 5-field cron expression
// after t.
func NextCron(expr string, after time.Time) (time.Time, error) {
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(after), nil
}
