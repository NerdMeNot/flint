package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/NerdMeNot/flint/internal/cliauth"
	"github.com/NerdMeNot/flint/internal/tui"
)

// user_commands.go — the developer-facing CLI verbs: login, logout, init,
// run, logs. Operator verbs (admin, dev, runner) live in their own files.

func loginCmd() *cobra.Command {
	var tokenFlag string
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with a Flint server (device flow, or --token for a PAT)",
		RunE: func(cmd *cobra.Command, args []string) error {
			server := serverURL
			if server == "" {
				server = os.Getenv("FLINT_SERVER_URL")
			}
			if server == "" {
				server = "http://localhost:8080"
			}
			server = strings.TrimRight(server, "/")

			// PAT path: paste a token, verify it, store it.
			if tokenFlag != "" {
				creds := &cliauth.Credentials{ServerURL: server, Token: tokenFlag}
				if err := verifyCreds(cmd.Context(), creds); err != nil {
					return fmt.Errorf("token rejected by %s: %w", server, err)
				}
				if err := cliauth.Save(creds); err != nil {
					return err
				}
				fmt.Printf("✓ Logged in to %s (personal access token)\n", server)
				return nil
			}

			// Device flow.
			creds, err := cliauth.DeviceLogin(cmd.Context(), server, func(userCode, uri string) {
				fmt.Printf("\nTo authenticate, open:\n\n    %s\n\nand enter the code:\n\n    %s\n\nWaiting for approval...\n", uri, userCode)
			})
			if err != nil {
				return err
			}
			_ = creds
			fmt.Printf("\n✓ Logged in to %s\n", server)
			return nil
		},
	}
	cmd.Flags().StringVar(&tokenFlag, "token", "", "personal access token (flint_pat_...) instead of the device flow")
	return cmd
}

func logoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Remove stored credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := cliauth.Delete(); err != nil {
				return err
			}
			fmt.Println("✓ Logged out")
			return nil
		},
	}
}

// verifyCreds makes an authenticated call to confirm a token works.
func verifyCreds(ctx context.Context, creds *cliauth.Credentials) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, creds.ServerURL+"/api/v1/org", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+creds.Token)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

const initTemplate = `# Flint pipeline — https://github.com/NerdMeNot/flint
# Editor autocomplete: point yaml-language-server at your server's schema:
# yaml-language-server: $schema=%s/schemas/pipeline.json
image: alpine:3.21

triggers:
  push: { branches: [main] }
  pull_request: { branches: [main] }

jobs:
  build:
    steps:
      - use: checkout
      - name: hello
        run: echo "hello from Flint"
`

func initPipelineCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init",
		Short: "Scaffold .flint/ci.yaml in the current repository",
		RunE: func(cmd *cobra.Command, args []string) error {
			server := "https://your-flint-server"
			if creds, err := cliauth.Load(); err == nil {
				server = creds.ServerURL
			}
			path := ".flint/ci.yaml"
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s already exists", path)
			}
			if err := os.MkdirAll(".flint", 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(path, []byte(fmt.Sprintf(initTemplate, server)), 0o644); err != nil {
				return err
			}
			fmt.Printf("✓ Created %s\n  Validate with: flint validate\n", path)
			return nil
		},
	}
}

// authedClient builds a TUI API client from stored credentials.
func authedClient(ctx context.Context) (*tui.Client, error) {
	creds, err := cliauth.Load()
	if err != nil {
		return nil, err
	}
	if serverURL != "" {
		creds.ServerURL = strings.TrimRight(serverURL, "/")
	}
	return tui.NewAuthedClient(creds), nil
}

func runCmd() *cobra.Command {
	var project, branch, file, env string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Trigger a pipeline run",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			client, err := authedClient(ctx)
			if err != nil {
				return err
			}
			if project == "" {
				return fmt.Errorf("--project is required (name or id)")
			}
			projectID, err := client.ResolveProject(ctx, project)
			if err != nil {
				return err
			}
			run, err := client.TriggerRun(ctx, projectID, branch, file, env)
			if err != nil {
				return err
			}
			fmt.Printf("✓ Run %s started\n  Follow logs: flint logs %s -f\n", run.ID, run.ID)
			return nil
		},
	}
	cmd.Flags().StringVarP(&project, "project", "p", "", "project name or id")
	cmd.Flags().StringVarP(&branch, "branch", "b", "", "branch (default: main)")
	cmd.Flags().StringVarP(&file, "file", "f", "", "pipeline file (default: ci.yaml)")
	cmd.Flags().StringVar(&env, "env", "", "target environment")
	return cmd
}

func logsCmd() *cobra.Command {
	var step string
	var follow bool
	cmd := &cobra.Command{
		Use:   "logs <run-id>",
		Short: "Print (or follow) a run's logs",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			client, err := authedClient(ctx)
			if err != nil {
				return err
			}
			runID := args[0]

			if follow {
				if step == "" {
					return followRunLogs(ctx, client, runID)
				}
				return client.StreamStepLogs(ctx, runID, step, os.Stdout)
			}

			if step != "" {
				lines, _, err := client.GetStepLogs(ctx, runID, step)
				if err != nil {
					return err
				}
				for _, l := range lines {
					fmt.Println(l.Content)
				}
				return nil
			}

			logs, err := client.GetRunLogs(ctx, runID)
			if err != nil {
				return err
			}
			for name, text := range logs {
				fmt.Printf("──— %s ———\n%s\n", name, text)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&step, "step", "s", "", "step (job) name")
	cmd.Flags().BoolVarP(&follow, "follow", "F", false, "stream logs live")
	return cmd
}

// followRunLogs follows a whole run: it watches the step states and streams
// each container step's logs in sequence as it starts.
func followRunLogs(ctx context.Context, client *tui.Client, runID string) error {
	streamed := map[string]bool{}
	for {
		state, err := client.GetRunSteps(ctx, runID)
		if err != nil {
			return err
		}
		terminal := state.Status == "succeeded" || state.Status == "failed" || state.Status == "cancelled"
		for _, s := range state.Steps {
			if streamed[s.Name] || s.Status == "pending" || s.Status == "queued" || s.Status == "skipped" {
				continue
			}
			if s.ExecType != "run" && s.ExecType != "use" && s.ExecType != "steps" {
				streamed[s.Name] = true
				continue
			}
			streamed[s.Name] = true
			fmt.Printf("\n──— %s ———\n", s.Name)
			if err := client.StreamStepLogs(ctx, runID, s.Name, os.Stdout); err != nil {
				fmt.Printf("(log stream ended: %v)\n", err)
			}
		}
		if terminal && allStreamedOrSkipped(state, streamed) {
			fmt.Printf("\nRun %s: %s\n", runID, state.Status)
			if state.Status != "succeeded" {
				os.Exit(1)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func allStreamedOrSkipped(state *tui.WorkflowState, streamed map[string]bool) bool {
	for _, s := range state.Steps {
		if !streamed[s.Name] && s.Status != "skipped" && s.Status != "pending" && s.Status != "cancelled" {
			return false
		}
	}
	return true
}
