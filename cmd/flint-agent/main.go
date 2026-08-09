package main

import (
	"context"
	"encoding/json"
	"os"
	"strings"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/NerdMeNot/flint/internal/agentd"
	"github.com/NerdMeNot/flint/internal/core/agent"
	"github.com/NerdMeNot/flint/internal/version"
	"github.com/NerdMeNot/flint/pkg/checkout"
)

func main() {
	root := &cobra.Command{
		Use:     "flint-agent",
		Short:   "Flint machine agent — joins a pool and executes CI steps",
		Version: version.String(),
	}

	root.AddCommand(daemonCmd(), checkoutCmd(), stepsCmd())

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

// stepsCmd runs the in-container steps driver: sequential sub-steps inside the
// user image for a group ("steps") job. The daemon bind-mounts this binary
// into the step container and executes it as the main process.
func stepsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "steps",
		Short: "Run a job's sub-steps sequentially (in-container driver)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := agent.LoadStepsConfig()
			if err != nil {
				return err
			}
			// The driver's exit code IS the job's result — the daemon reads it
			// off the container, so propagate the failing sub-step's code.
			if code := agent.RunSteps(context.Background(), cfg, os.Stdout); code != 0 {
				os.Exit(code)
			}
			return nil
		},
	}
}

// checkoutCmd performs git checkout as an explicit pipeline step.
// Developers write:
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

			// Read step inputs from FLINT_CHECKOUT_INPUTS env var (JSON map),
			// injected by the daemon when dispatching a use: checkout step.
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
