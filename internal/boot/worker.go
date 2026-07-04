// Package boot wires the Flint control plane (server, dispatch loop, IdP sync)
// from configuration. It is the composition root behind `flint server`:
//
//   - --mode all      — the single-binary install (Postgres + one process)
//   - --mode dispatch — the engine loop alone, for horizontal scale-out
//   - --mode sync     — the IdP sync loop alone
//
// Like cmd/*, this package may import core, platform, and products.
package boot

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/core/fleet"
	"github.com/NerdMeNot/flint/internal/core/runner"
	"github.com/NerdMeNot/flint/internal/platform/config"
	"github.com/NerdMeNot/flint/internal/products/workflows"
	"github.com/NerdMeNot/flint/pkg/logsink"
)

// Worker bundles the running worker components.
type Worker struct {
	Loop *engine.Loop
}

// StartWorker builds the executor registry, runner-pool refresh, and cron
// scheduler, and returns the loop ready to Run. Background goroutines are
// bound to ctx.
func StartWorker(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, eng *engine.PgEngine) (*Worker, error) {
	// Machine pool registry — loaded from the DB (the source of truth; no CRD/
	// controller). Configurable default pool for jobs without an explicit runner.
	runner.SetDefault(cfg.Engine.DefaultPoolOrDefault())
	registry := runner.NewRegistry()
	q := db.New(pool)
	if err := runner.LoadAll(ctx, q, registry); err != nil {
		log.Warn().Err(err).Msg("runner: failed to load pools from DB (will retry on refresh)")
	}
	// Periodic refresh so admin pool changes (via the API) reach the worker.
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := runner.LoadAll(ctx, q, registry); err != nil {
					log.Debug().Err(err).Msg("runner: pool refresh failed")
				}
			}
		}
	}()

	// Build the executor registry (step exec type → executor). Container steps
	// (run/use/steps) dispatch to the machine fleet: Dispatch persists a
	// step_assignments row, the fleet scheduler binds it, and a flint-agent
	// executes it.
	//
	// FLINT_EXECUTOR=sim swaps the container backend for the simulated
	// executor: no machines — steps sleep, emit synthetic logs, and report a
	// scripted outcome, letting the real engine drive runs to completion with
	// zero infrastructure (dev-sim, docker-compose demo).
	executors := engine.ExecutorRegistry{}
	var container engine.StepExecutor
	if os.Getenv("FLINT_EXECUTOR") == "sim" {
		container = engine.NewSimExecutor(eng.CompleteStep, &logsink.FilesystemSink{BaseDir: cfg.Storage.FS.Path})
		log.Info().Str("logPath", cfg.Storage.FS.Path).Msg("container executor: sim (no machines)")
	} else {
		execCfg := engine.MachineExecutorConfig{ServerHTTPURL: cfg.Server.BaseURL}
		if cfg.Storage.Mode == "s3" {
			execCfg.S3Bucket = cfg.Storage.S3.Bucket
			execCfg.S3Region = cfg.Storage.S3.Region
			execCfg.S3Endpoint = cfg.Storage.S3.Endpoint
		}
		container = engine.NewMachineExecutor(pool, registry, execCfg)
		log.Info().Msg("container executor: machine fleet")
	}
	executors["run"] = container
	executors["use"] = container
	executors["steps"] = container
	// http steps run in-process regardless of the container backend.
	executors["http"] = engine.NewHTTPExecutor(eng.CompleteStep)

	// Fleet loop — lease sweeps, unclaimed releases, assignment scheduling,
	// provisioning, and scale-down. Runs beside the engine loop.
	fl := fleet.New(pool, eng)
	if mk, err := cfg.Encryption.DecodeMasterKey(); err == nil {
		fl.SetMasterKey(mk)
	}
	fl.SetBootstrapEndpoints(fleetBootstrapEndpoints(cfg))
	go func() {
		if err := fleet.NewLoop(fl, fleet.LoopConfig{}).Run(ctx); err != nil {
			log.Error().Err(err).Msg("fleet: loop stopped with error")
		}
	}()

	// Worker loop — polls Postgres, dispatches steps via the registry, fires timers.
	loop := engine.NewLoop(eng, executors, engine.LoopConfig{
		SweepInterval:    cfg.Engine.SweepIntervalOrDefault(),
		SigningKey:       []byte(cfg.Auth.JWT.Secret),
		RunRetentionDays: cfg.Engine.RunRetentionDays,
	})

	// Workflow cron scheduler — fires due schedules into the engine. Runs
	// alongside the loop when the Workflows product is enabled.
	if cfg.Products.WorkflowsEnabled() {
		go workflows.NewScheduler(db.New(pool), eng).Run(ctx)
		log.Info().Msg("workflows: cron scheduler enabled")
	}

	return &Worker{Loop: loop}, nil
}

// fleetBootstrapEndpoints derives what elastic machines need to find Flint:
// the gRPC control channel (baseUrl host + grpcPort), the HTTP data plane
// (baseUrl), and where to download the agent binary (GitHub releases; a
// self-hosted mirror can override via the pool's provider config later).
func fleetBootstrapEndpoints(cfg *config.Config) fleet.BootstrapEndpoints {
	grpcURL := ""
	if base := cfg.Server.BaseURL; base != "" {
		host := strings.TrimPrefix(strings.TrimPrefix(base, "https://"), "http://")
		if i := strings.IndexAny(host, ":/"); i > 0 {
			host = host[:i]
		}
		grpcURL = fmt.Sprintf("%s:%d", host, cfg.Server.GRPCPortOrDefault())
	}
	return fleet.BootstrapEndpoints{
		ServerGRPCURL:    grpcURL,
		ServerHTTPURL:    cfg.Server.BaseURL,
		AgentDownloadURL: "https://github.com/NerdMeNot/flint/releases/latest/download",
	}
}
