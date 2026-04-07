package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/NerdMeNot/flint/internal/db"
	"github.com/NerdMeNot/flint/internal/runner"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"
	"k8s.io/client-go/kubernetes"
)

// Loop is the worker main loop. It polls Postgres for queued steps,
// fires timers, dispatches K8s Jobs, and sweeps for stale state.
type Loop struct {
	pool         *pgxpool.Pool
	engine       *PgEngine
	k8s          kubernetes.Interface
	registry     *runner.Registry
	config       LoopConfig
	agentImage   string
	jobNamespace string
	serverURL    string
	wake         chan struct{}
}

// NewLoop creates a worker loop.
func NewLoop(engine *PgEngine, k8s kubernetes.Interface, reg *runner.Registry, cfg LoopConfig,
	agentImage, jobNamespace, serverURL string) *Loop {
	return &Loop{
		pool:         engine.pool,
		engine:       engine,
		k8s:          k8s,
		registry:     reg,
		config:       cfg,
		agentImage:   agentImage,
		jobNamespace: jobNamespace,
		serverURL:    serverURL,
		wake:         make(chan struct{}, 1),
	}
}

// Run starts the main loop. Blocks until context cancellation.
func (l *Loop) Run(ctx context.Context) error {
	log.Info().
		Dur("pollInterval", l.config.pollInterval()).
		Dur("sweepInterval", l.config.sweepInterval()).
		Msg("engine: worker loop started")

	// Start LISTEN/NOTIFY listener for instant wakeup.
	go l.listenNotify(ctx)

	ticker := time.NewTicker(l.config.pollInterval())
	sweepTicker := time.NewTicker(l.config.sweepInterval())
	defer ticker.Stop()
	defer sweepTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("engine: worker loop stopped")
			return nil
		case <-l.wake:
			l.tick(ctx)
		case <-ticker.C:
			l.tick(ctx)
		case <-sweepTicker.C:
			l.sweep(ctx)
		}
	}
}

// tick is one iteration of the main loop.
func (l *Loop) tick(ctx context.Context) {
	// Phase 1: Fire due timers.
	if err := fireTimers(ctx, l.pool); err != nil {
		log.Error().Err(err).Msg("engine: fire timers error")
	}

	// Phase 2: Process gate approval signals.
	l.processSignals(ctx)

	// Phase 3: Claim and dispatch queued steps.
	l.claimAndDispatch(ctx)
}

// claimAndDispatch claims queued steps and dispatches them.
func (l *Loop) claimAndDispatch(ctx context.Context) {
	l.claimAndDispatchSimple(ctx)
}

func (l *Loop) claimAndDispatchSimple(ctx context.Context) {
	q := db.New(l.pool)
	claimedSteps, err := q.ClaimQueuedSteps(ctx, int32(l.config.claimBatchSize()))
	if err != nil {
		log.Error().Err(err).Msg("engine: claim steps error")
		return
	}

	// For each claimed step, generate task token, fetch workflow context, dispatch.
	for _, c := range claimedSteps {
		token := EncodeTaskToken(TaskToken{
			WorkflowID: c.WorkflowID,
			StepName:   c.Name,
			Attempt:    int(c.Attempt),
		})

		// Update task token on the step.
		_ = q.SetStepTaskToken(ctx, db.SetStepTaskTokenParams{
			ID:        c.ID,
			TaskToken: &token,
		})

		// Fetch workflow input for context.
		inputJSON, _ := q.GetWorkflowInput(ctx, c.WorkflowID)
		var input StartWorkflowInput
		_ = json.Unmarshal(inputJSON, &input)

		// Create timers for gate steps.
		if c.ExecType == "gate" {
			l.createStepTimers(ctx, c.WorkflowID, c.Name)
		}

		// Dispatch run/use/steps steps as K8s Jobs.
		if c.Status == "running" && l.k8s != nil {
			step := claimedStep{
				id: c.ID, workflowID: c.WorkflowID, name: c.Name,
				execType: c.ExecType, taskToken: token, stepDef: c.StepDef,
				runID: input.RunID, orgID: input.OrgID,
				repo: input.Repo, ref: input.Ref, commitSHA: input.CommitSHA,
				env: input.Env,
			}
			if err := dispatchStep(ctx, l.k8s, l.registry, step, l.agentImage, l.jobNamespace, l.serverURL); err != nil {
				log.Error().Err(err).Str("step", c.Name).Msg("engine: dispatch failed")
				// Mark step as failed.
				_ = q.UpdateStepResult(ctx, db.UpdateStepResultParams{
					ID:     c.ID,
					Status: "failed",
					Result: mustJSON(StepResult{StepName: c.Name, Success: false, Error: err.Error()}),
				})
			} else {
				// Update with K8s job name.
				jobName := fmt.Sprintf("flint-%s-%s", input.RunID[:8], c.Name)
				jn := jobName
				_ = q.SetStepK8sJobName(ctx, db.SetStepK8sJobNameParams{
					ID:         c.ID,
					K8sJobName: &jn,
				})
			}
		}
	}
}

// createStepTimers creates timers for gate steps.
func (l *Loop) createStepTimers(ctx context.Context, workflowID, stepName string) {
	q := db.New(l.pool)

	// Gate no longer has a configurable timeout — use a sensible default.
	timeout := 4 * time.Hour
	_ = q.CreateTimer(ctx, db.CreateTimerParams{
		WorkflowID: workflowID,
		StepName:   stepName,
		TimerType:  "gate_timeout",
		Secs:       timeout.Seconds(),
	})
}

// processSignals checks for gate approval signals on waiting steps.
func (l *Loop) processSignals(ctx context.Context) {
	q := db.New(l.pool)
	approvals, err := q.ListWaitingGatesWithSignals(ctx)
	if err != nil {
		return
	}

	for _, a := range approvals {
		tx, err := l.pool.Begin(ctx)
		if err != nil {
			continue
		}
		qtx := db.New(l.pool).WithTx(tx)

		_ = qtx.ConsumeSignal(ctx, a.SignalID)
		_ = qtx.UpdateStepResult(ctx, db.UpdateStepResultParams{
			ID:     a.StepID,
			Status: "succeeded",
			Result: mustJSON(StepResult{StepName: a.StepName, Success: true}),
		})
		_ = qtx.CancelTimer(ctx, db.CancelTimerParams{
			WorkflowID: a.WorkflowID,
			StepName:   a.StepName,
			TimerType:  "gate_timeout",
		})
		_ = advanceWorkflow(ctx, qtx, a.WorkflowID, 0)

		if err := tx.Commit(ctx); err != nil {
			tx.Rollback(ctx)
		}
		log.Info().Str("step", a.StepName).Msg("engine: gate approved")
	}
}

// sweep detects and recovers from stale state.
func (l *Loop) sweep(ctx context.Context) {
	log.Debug().Msg("engine: sweep started")

	// 1. Stale running steps past their deadline.
	q := db.New(l.pool)
	count, err := q.SweepStaleRunningSteps(ctx)
	if err == nil && count > 0 {
		log.Warn().Int64("count", count).Msg("engine: sweep recovered stale steps")

		wfIDs, _ := q.RecentlyFailedWorkflowIDs(ctx)
		for _, wfID := range wfIDs {
			tx, _ := l.pool.Begin(ctx)
			if tx != nil {
				qtx := db.New(l.pool).WithTx(tx)
				_ = advanceWorkflow(ctx, qtx, wfID, 0)
				_ = tx.Commit(ctx)
			}
		}
	}

	// 2. Stale workflows where all steps are terminal but workflow still "running".
	_ = q.SweepStaleWorkflows(ctx)

	// 3. Clean up fired timers older than 1 hour.
	_ = q.CleanupFiredTimers(ctx)

	log.Debug().Msg("engine: sweep completed")
}

// listenNotify listens for Postgres NOTIFY events for instant wakeup.
func (l *Loop) listenNotify(ctx context.Context) {
	for {
		conn, err := l.pool.Acquire(ctx)
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

		for {
			_, err := conn.Conn().WaitForNotification(ctx)
			if err != nil {
				conn.Release()
				if ctx.Err() != nil {
					return
				}
				break // reconnect
			}
			select {
			case l.wake <- struct{}{}:
			default:
			}
		}
	}
}
