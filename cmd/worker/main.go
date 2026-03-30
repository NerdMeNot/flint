package main

import (
	"context"
	"fmt"
	"os"

	"github.com/NerdMeNot/flint/internal/config"
	"github.com/NerdMeNot/flint/internal/dbkit"
	"github.com/NerdMeNot/flint/internal/engine"
	"github.com/NerdMeNot/flint/internal/observe"
	"github.com/NerdMeNot/flint/internal/runner"
	workerinformer "github.com/NerdMeNot/flint/internal/worker/informer"
	"github.com/NerdMeNot/flint/pkg/forge"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	version    = "dev"
	commit     = "unknown"
	configPath string
)

func main() {
	root := &cobra.Command{
		Use:     "flint-worker",
		Short:   "Flint CI worker — workflow engine, K8s job orchestration",
		Version: fmt.Sprintf("%s (%s)", version, commit),
		RunE:    run,
	}

	root.Flags().StringVar(&configPath, "config", "", "path to config file")

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if err := config.Validate(cfg, "worker"); err != nil {
		return fmt.Errorf("config validation: %w", err)
	}

	shutdown, err := observe.Init(ctx, observe.Config{
		ServiceName:    "flint-worker",
		ServiceVersion: version,
		LogLevel:       "info",
	})
	if err != nil {
		return fmt.Errorf("observability init: %w", err)
	}
	defer shutdown(ctx)

	log.Info().
		Str("version", version).
		Str("jobNamespace", cfg.Worker.JobNamespaceOrDefault()).
		Msg("flint-worker starting")

	// Database.
	pool, err := dbkit.NewPool(ctx, dbkit.Config{
		Host:     cfg.Database.Host,
		Port:     cfg.Database.PortOrDefault(),
		Database: cfg.Database.Database,
		User:     cfg.Database.User,
		Password: cfg.Database.Password,
		SSLMode:  cfg.Database.SSLMode,
	})
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()

	// Forge provider.
	forgeProvider := forge.NewGitHub("", nil)

	// Engine.
	eng := engine.New(pool, forgeProvider)
	defer eng.Close()

	// Runner pool registry.
	registry := runner.NewRegistry()

	// K8s client — initialized in-cluster at runtime.
	// For now nil — the loop handles nil k8s gracefully.
	// In production: k8s := kubernetes.NewForConfigOrDie(rest.InClusterConfig())

	// Worker loop — polls Postgres, creates K8s Jobs, fires timers.
	loop := engine.NewLoop(eng, nil, registry, engine.LoopConfig{
		SweepInterval: cfg.Worker.SweepIntervalOrDefault(),
	}, cfg.Worker.AgentImage, cfg.Worker.JobNamespaceOrDefault(),
		fmt.Sprintf("http://flint-server.flint:%d", cfg.Server.PortOrDefault()),
	)

	// Start K8s informer in background.
	go func() {
		// Informer now calls engine.CompleteStep directly instead of Temporal signals.
		watcher := workerinformer.New(nil, nil, workerinformer.Config{
			Namespace: cfg.Worker.JobNamespaceOrDefault(),
		})
		if err := watcher.Run(ctx); err != nil {
			log.Error().Err(err).Msg("K8s informer stopped with error")
		}
	}()

	log.Info().Msg("flint-worker ready, starting engine loop")
	return loop.Run(ctx)
}
