package main

import (
	"context"
	"fmt"
	"os"

	"github.com/NerdMeNot/flint/internal/agent"
	"github.com/NerdMeNot/flint/pkg/logsink"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	root := &cobra.Command{
		Use:     "flint-agent",
		Short:   "Flint CI agent — sidecar for step execution in K8s Jobs",
		Version: fmt.Sprintf("%s (%s)", version, commit),
	}

	root.AddCommand(initCmd(), watchCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// initCmd clones the repo into the shared workspace.
// Runs as a K8s init container before the step container starts.
func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Clone repo into workspace (init container)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			cfg, err := agent.LoadFromEnv()
			if err != nil {
				return err
			}

			log.Info().
				Str("version", version).
				Str("repo", cfg.GitRepo).
				Str("ref", cfg.GitRef).
				Str("workspace", cfg.Workspace).
				Msg("flint-agent init")

			// Fetch secrets before clone (clone may need credentials).
			if err := agent.FetchSecrets(ctx, cfg); err != nil {
				log.Warn().Err(err).Msg("failed to fetch secrets (continuing)")
			}

			if err := agent.Clone(ctx, cfg); err != nil {
				agent.ReportError(cfg, err)
				return err
			}

			return nil
		},
	}
}

// watchCmd monitors the step container, streams logs, and reports completion.
// Runs as a K8s sidecar alongside the step container.
func watchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch",
		Short: "Watch step container, stream logs, report completion (sidecar)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			cfg, err := agent.LoadFromEnv()
			if err != nil {
				return err
			}

			log.Info().
				Str("version", version).
				Str("step", cfg.StepName).
				Str("runID", cfg.RunID).
				Msg("flint-agent watch")

			// Build log sink from env config.
			var sink logsink.LogSink
			switch cfg.LogSinkMode {
			case "filesystem":
				sink = &logsink.FilesystemSink{BaseDir: cfg.FSLogPath}
			default:
				// Default to filesystem in /tmp for dev.
				sink = &logsink.FilesystemSink{BaseDir: "/tmp/flint-logs"}
			}

			if err := agent.Watch(ctx, cfg, sink); err != nil {
				return err
			}

			return nil
		},
	}
}
