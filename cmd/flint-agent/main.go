package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/NerdMeNot/flint/internal/agentd"
	"github.com/NerdMeNot/flint/internal/core/agent"
	"github.com/NerdMeNot/flint/internal/core/wsagent"
	"github.com/NerdMeNot/flint/internal/version"
	"github.com/NerdMeNot/flint/pkg/checkout"
	"github.com/NerdMeNot/flint/pkg/logsink"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:     "flint-agent",
		Short:   "Flint machine agent — joins a pool and executes CI steps",
		Version: version.String(),
	}

	root.AddCommand(daemonCmd(), sidecarCmd(), initCmd(), watchCmd(), workspaceCmd(), checkoutCmd(), stepsCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// daemonCmd runs the persistent machine agent: register with a pool
// join/bootstrap token, heartbeat, claim assigned steps, execute them.
func daemonCmd() *cobra.Command {
	var cfg agentd.Config
	var labels []string

	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Run the persistent machine agent (register, heartbeat, execute steps)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(labels) > 0 {
				cfg.Labels = map[string]string{}
				for _, l := range labels {
					k, v, _ := strings.Cut(l, "=")
					cfg.Labels[k] = v
				}
			}
			log.Info().Str("version", version.String()).Msg("flint-agent daemon starting")
			return agentd.Run(cmd.Context(), cfg)
		},
	}

	cmd.Flags().StringVar(&cfg.ServerURL, "server", "", "control plane gRPC address (host:port)")
	cmd.Flags().StringVar(&cfg.Token, "token", "", "pool join token or bootstrap token (unused once registered)")
	cmd.Flags().StringVar(&cfg.MachineID, "machine-id", "", "pre-allocated machine id (elastic machines)")
	cmd.Flags().StringVar(&cfg.DataDir, "data-dir", "", "agent state root (default /var/lib/flint-agent)")
	cmd.Flags().IntVar(&cfg.Capacity, "capacity", 0, "max concurrent steps (default NumCPU/2)")
	cmd.Flags().StringVar(&cfg.Runtime, "runtime", "", "execution runtime: containerd (default) | hostshell (dev only, no isolation)")
	cmd.Flags().StringSliceVar(&labels, "label", nil, "capability label key=value (repeatable)")
	cmd.Flags().BoolVar(&cfg.Insecure, "insecure", false, "dial gRPC without TLS (dev/local)")
	return cmd
}

// sidecarCmd is the single per-step agent container (native sidecar): it
// prepares the workspace (init phase), writes the init-done marker the step
// container's startup probe gates on, then watches the step (logs, sync-out,
// artifact/cache upload, completion). One container instead of the previous
// init + sidecar pair — one fewer resource request per step pod.
func sidecarCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sidecar",
		Short: "Prepare workspace, then watch the step (single per-step container)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			cfg, err := agent.LoadFromEnv()
			if err != nil {
				return err
			}

			log.Info().
				Str("version", version.String()).
				Str("step", cfg.StepName).
				Str("workspace", cfg.Workspace).
				Msg("flint-agent sidecar")

			if err := agent.Init(ctx, cfg); err != nil {
				// Init failure means the step must not start — report and exit
				// nonzero so the informer/engine resolve the step.
				agent.ReportError(cfg, err)
				return err
			}
			if err := agent.MarkInitDone(cfg.Workspace); err != nil {
				agent.ReportError(cfg, err)
				return err
			}

			return agent.Watch(ctx, cfg, buildLogSink(cfg))
		},
	}
}

// initCmd runs only the workspace-preparation phase (debugging aid; production
// pods use the combined sidecar command).
func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Set up workspace only (debug; production uses sidecar)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := agent.LoadFromEnv()
			if err != nil {
				return err
			}
			return agent.Init(context.Background(), cfg)
		},
	}
}

// buildLogSink builds the log sink from env config. The default ("server")
// ships batched lines to flint-server /internal/logs — the server owns durable
// storage and fans out to SSE subscribers for live tail. A pod-local
// filesystem sink would die with the pod, so it is only for tests/debugging
// via FLINT_LOG_SINK=filesystem.
func buildLogSink(cfg *agent.Config) logsink.LogSink {
	switch cfg.LogSinkMode {
	case "filesystem":
		fsPath := cfg.FSLogPath
		if fsPath == "" {
			fsPath = "/tmp/flint-logs"
		}
		return &logsink.FilesystemSink{BaseDir: fsPath}
	default:
		return &logsink.HTTPSink{
			ServerURL:     cfg.ServerURL,
			TaskToken:     cfg.TaskToken,
			InternalToken: cfg.InternalToken,
		}
	}
}

// watchCmd runs only the watch phase (debugging aid; production pods use the
// combined sidecar command).
func watchCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "watch",
		Short: "Watch step container only (debug; production uses sidecar)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := agent.LoadFromEnv()
			if err != nil {
				return err
			}
			return agent.Watch(context.Background(), cfg, buildLogSink(cfg))
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
				Str("version", version.String()).
				Str("runID", runID).
				Str("addr", addr).
				Str("root", root).
				Msg("flint-agent workspace")

			return wsagent.ListenAndServe(ctx, addr, token, root)
		},
	}
}

// stepsCmd runs the in-pod steps driver: sequential sub-steps inside the user
// image for a group ("steps") job. Executed as the step container's main
// process from the workspace copy installed by the init container.
func stepsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "steps",
		Short: "Run a job's sub-steps sequentially (in-pod driver)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := agent.LoadStepsConfig()
			if err != nil {
				return err
			}
			return agent.RunSteps(context.Background(), cfg)
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
				Str("version", version.String()).
				Str("repo", opts.Repo).
				Str("ref", opts.Ref).
				Str("path", opts.Path).
				Msg("flint-agent checkout")

			return checkout.Run(ctx, opts)
		},
	}
}
