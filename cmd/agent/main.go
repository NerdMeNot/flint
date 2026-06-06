package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/NerdMeNot/flint/internal/agent"
	"github.com/NerdMeNot/flint/internal/wsagent"
	"github.com/NerdMeNot/flint/pkg/artifact"
	pkgcache "github.com/NerdMeNot/flint/pkg/cache"
	"github.com/NerdMeNot/flint/pkg/checkout"
	"github.com/NerdMeNot/flint/pkg/logsink"
	"github.com/NerdMeNot/flint/pkg/workspace"
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

	root.AddCommand(initCmd(), watchCmd(), workspaceCmd(), checkoutCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// initCmd sets up the workspace before the step container runs.
// Runs as a K8s init container. Handles: secrets, workspace sync, artifacts, cache.
// NOTE: Clone is no longer done here — use `use: checkout` as an explicit step.
func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Set up workspace (init container)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			cfg, err := agent.LoadFromEnv()
			if err != nil {
				return err
			}

			log.Info().
				Str("version", version).
				Str("step", cfg.StepName).
				Str("workspace", cfg.Workspace).
				Msg("flint-agent init")

			// Fetch secrets (needed before checkout for git tokens, AWS creds, etc).
			if err := agent.FetchSecrets(ctx, cfg); err != nil {
				log.Warn().Err(err).Msg("failed to fetch secrets (continuing)")
			}

			// Workspace sync: pull previous step's state if available.
			ws, wsErr := workspace.New(
				cfg.WorkspaceMode, cfg.WorkspaceAddr, cfg.WorkspaceToken,
				cfg.S3Bucket, cfg.S3Region, cfg.RunID,
			)
			if wsErr != nil {
				log.Warn().Err(wsErr).Msg("agent: failed to init workspace (skipping sync)")
			}
			if ws != nil {
				defer ws.Close()
				if _, syncErr := ws.SyncIn(ctx, cfg.Workspace); syncErr != nil {
					log.Warn().Err(syncErr).Msg("agent: SyncIn failed (continuing)")
				}
			}

			// Download artifact inputs from upstream steps.
			if cfg.S3Bucket != "" && len(cfg.ArtifactInputs) > 0 {
				store := artifact.NewS3Store(cfg.S3Bucket, cfg.S3Region)
				for _, input := range cfg.ArtifactInputs {
					ref := artifact.Ref{
						OrgID: cfg.OrgID, RunID: cfg.RunID,
						StepName: input.From, Name: input.Path,
					}
					if err := store.Download(ctx, ref, input.Path); err != nil {
						log.Warn().Err(err).Str("from", input.From).Msg("failed to download artifact (continuing)")
					}
				}
			}

			// Restore cache (always S3-backed for cross-run persistence).
			// NOTE: Cache key evaluation should happen AFTER checkout — if this
			// is the first step (before checkout), the key will miss. That's OK;
			// cache is best-effort.
			if cfg.S3Bucket != "" && cfg.CacheKey != "" {
				cacheStore := pkgcache.NewS3(cfg.OrgID, cfg.ProjectID, cfg.S3Bucket, cfg.S3Region)
				key, keyErr := agent.EvaluateCacheKey(cfg)
				if keyErr != nil {
					log.Warn().Err(keyErr).Msg("agent: failed to evaluate cache key (skipping)")
				} else {
					if _, err := cacheStore.Restore(ctx, key, cfg.CachePaths); err != nil {
						log.Warn().Err(err).Msg("failed to restore cache (continuing)")
					}
				}
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
				sink = &logsink.FilesystemSink{BaseDir: "/tmp/flint-logs"}
			}

			if err := agent.Watch(ctx, cfg, sink); err != nil {
				return err
			}

			return nil
		},
	}
}

// workspaceCmd runs the per-run workspace gRPC file server.
// Scheduled by the engine as a dedicated pod before any steps are dispatched.
func workspaceCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "workspace",
		Short: "Run the per-run workspace gRPC file server (workspace pod)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			runID := os.Getenv("FLINT_RUN_ID")
			token := os.Getenv("FLINT_WS_TOKEN")
			port := os.Getenv("FLINT_WS_PORT")
			root := os.Getenv("FLINT_WS_ROOT")

			if runID == "" {
				return fmt.Errorf("FLINT_RUN_ID is required")
			}
			if token == "" {
				return fmt.Errorf("FLINT_WS_TOKEN is required")
			}
			if port == "" {
				port = "7700"
			}
			if root == "" {
				root = "/workspace"
			}

			addr := "0.0.0.0:" + port

			log.Info().
				Str("version", version).
				Str("runID", runID).
				Str("addr", addr).
				Str("root", root).
				Msg("flint-agent workspace")

			return wsagent.ListenAndServe(ctx, addr, token, root)
		},
	}
}

// checkoutCmd performs git checkout as an explicit pipeline step.
// This replaces the implicit clone in the init container. Developers write:
//
//	steps:
//	  - use: checkout
//	  - name: test
//	    run: npm test
func checkoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "checkout",
		Short: "Clone repository into workspace (explicit step)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Read step inputs from FLINT_CHECKOUT_INPUTS env var (JSON map).
			// This is injected by the engine when dispatching a use: checkout step.
			inputs := make(map[string]string)
			if raw := os.Getenv("FLINT_CHECKOUT_INPUTS"); raw != "" {
				_ = json.Unmarshal([]byte(raw), &inputs)
			}

			opts := checkout.FromEnvAndInputs(inputs)

			log.Info().
				Str("version", version).
				Str("repo", opts.Repo).
				Str("ref", opts.Ref).
				Str("path", opts.Path).
				Msg("flint-agent checkout")

			return checkout.Run(ctx, opts)
		},
	}
}
