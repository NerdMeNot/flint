package main

import (
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/NerdMeNot/flint/internal/boot"
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/NerdMeNot/flint/internal/platform/config"
	"github.com/NerdMeNot/flint/internal/version"
)

// serverCmd runs the Flint control plane. `flint server` with no flags is the
// single-binary install: API + webhooks + dispatch loop + IdP sync in one
// process, Postgres the only dependency.
func serverCmd() *cobra.Command {
	var configPath, mode string

	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run the Flint control plane (API, webhooks, dispatch)",
		Long: `Runs the Flint control plane. Modes:

  all       API + webhooks + embedded dispatch loop + IdP sync (default —
            the single-binary install; Postgres + this process is everything)
  api       HTTP API only (scale-out; pair with dispatch replicas)
  webhook   webhook ingestion only
  dispatch  the engine loop only (horizontal scale-out for step dispatch)
  sync      the IdP session/group sync loop only`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			component := "server"
			if mode == "dispatch" {
				component = "dispatch"
			}
			if err := config.Validate(cfg, component); err != nil {
				return fmt.Errorf("config validation: %w", err)
			}

			shutdown, err := observe.Init(ctx, observe.Config{
				ServiceName:    "flint-" + mode,
				ServiceVersion: version.Version,
				LogLevel:       "info",
			})
			if err != nil {
				return fmt.Errorf("observability init: %w", err)
			}
			defer func() { _ = shutdown(ctx) }()

			log.Info().
				Str("version", version.String()).
				Str("mode", mode).
				Int("port", cfg.Server.PortOrDefault()).
				Msg("flint server starting")

			return boot.Run(ctx, cfg, mode)
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to config file")
	cmd.Flags().StringVar(&mode, "mode", "all", "server mode: all | api | webhook | dispatch | sync")
	return cmd
}
