package main

import (
	"context"
	"fmt"
	"os"

	"github.com/NerdMeNot/flint/internal/boot"
	"github.com/NerdMeNot/flint/internal/core/dbkit"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/NerdMeNot/flint/internal/platform/config"
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

// run wires the dedicated worker binary. Single-binary installs don't need it:
// flint-server --mode all runs the same loop in-process (internal/boot); this
// binary exists for scale-out deployments that separate dispatch from the API.
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

	// Engine. The JWT secret signs/verifies task tokens (must match the server;
	// never injected into step pods, unlike the internal token).
	eng := engine.New(pool, []byte(cfg.Auth.JWT.Secret))
	defer eng.Close()

	w, err := boot.StartWorker(ctx, cfg, pool, eng)
	if err != nil {
		return fmt.Errorf("starting worker: %w", err)
	}

	log.Info().Msg("flint-worker ready, starting engine loop")
	return w.Loop.Run(ctx)
}
