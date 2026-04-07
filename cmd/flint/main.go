package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/NerdMeNot/flint/internal/tui"
	"github.com/NerdMeNot/flint/pkg/pipeline"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var (
	version   = "dev"
	commit    = "unknown"
	serverURL string
	runID     string
	project   string
	gates     bool
)

func main() {
	root := &cobra.Command{
		Use:   "flint",
		Short: "Flint — the CI platform your Kubernetes cluster deserves",
		Long: `Flint is a Kubernetes-native CI platform built on Temporal.
Durable by default, K8s-native, and not Jenkins.`,
		Version: fmt.Sprintf("%s (%s)", version, commit),
		RunE:    runTUI,
	}

	root.PersistentFlags().StringVar(&serverURL, "server", "", "Flint server URL (default: $FLINT_SERVER_URL or http://localhost:8080)")
	root.Flags().StringVar(&runID, "run", "", "jump to run detail by ID")
	root.Flags().StringVar(&project, "project", "", "jump to project by name")
	root.Flags().BoolVar(&gates, "gates", false, "show pending gate approvals")

	root.AddCommand(validateCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func runTUI(cmd *cobra.Command, args []string) error {
	base := serverURL
	if base == "" {
		base = os.Getenv("FLINT_SERVER_URL")
	}
	if base == "" {
		base = "http://localhost:8080"
	}

	client := tui.NewClient(base)
	app := tui.NewApp(client, tui.StartOption{
		RunID:   runID,
		Project: project,
		Gates:   gates,
	})

	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func validateCmd() *cobra.Command {
	var envFlag string

	cmd := &cobra.Command{
		Use:   "validate [path]",
		Short: "Validate pipeline YAML files",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := ".flint/"
			if len(args) > 0 {
				dir = args[0]
			}

			files, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
			if err != nil {
				return fmt.Errorf("reading %s: %w", dir, err)
			}
			ymlFiles, _ := filepath.Glob(filepath.Join(dir, "*.yml"))
			files = append(files, ymlFiles...)

			if len(files) == 0 {
				return fmt.Errorf("no pipeline YAML files found in %s", dir)
			}

			hasErrors := false
			for _, f := range files {
				data, err := os.ReadFile(f)
				if err != nil {
					fmt.Printf("  ✗ %s: %v\n", f, err)
					hasErrors = true
					continue
				}

				// Rich validation.
				result := pipeline.Validate(data, pipeline.ValidateOptions{})

				if !result.Valid() {
					fmt.Printf("  ✗ %s — %d error(s)\n", f, len(result.Errors()))
					for _, issue := range result.Errors() {
						printIssue(issue)
					}
					for _, issue := range result.Warnings() {
						printIssue(issue)
					}
					hasErrors = true
					continue
				}

				// Parse and resolve DAG for summary.
				p, _ := pipeline.Parse(data)
				waves, _ := pipeline.ResolveDag(p)

				envAware := ""
				if pipeline.IsEnvironmentAware(p) {
					envAware = " [env-aware]"
				}
				fmt.Printf("  ✓ %s — %d steps, %d waves%s\n", f, len(p.Steps), len(waves), envAware)

				// Warnings.
				for _, issue := range result.Warnings() {
					printIssue(issue)
				}

				// Environment simulation if requested.
				if envFlag != "" && p != nil {
					sim := pipeline.SimulateEnv(p, envFlag)
					fmt.Printf("\n    Preview: %s → %s\n", f, envFlag)
					fmt.Printf("    Triggers:\n")
					for _, t := range sim.ActiveTriggers {
						marker := "·"
						if t.Active {
							marker = "✓"
						}
						fmt.Printf("      %s %s — %s\n", marker, t.Type, t.Reason)
					}
					fmt.Printf("    Steps:\n")
					for _, s := range sim.Steps {
						if s.Active {
							fmt.Printf("      ✓ %-20s %s\n", s.Name, s.ExecType)
						} else {
							fmt.Printf("      ○ %-20s skipped — %s\n", s.Name, s.SkipReason)
						}
					}
				}
			}

			if hasErrors {
				return fmt.Errorf("validation failed")
			}
			fmt.Printf("\n✓ All %d pipelines valid\n", len(files))
			return nil
		},
	}

	cmd.Flags().StringVar(&envFlag, "env", "", "Simulate pipeline for a specific environment")
	return cmd
}

func printIssue(issue pipeline.ValidationIssue) {
	prefix := "    ⚠"
	if issue.Severity == pipeline.SeverityError {
		prefix = "    ✗"
	}
	loc := ""
	if issue.Line > 0 {
		loc = fmt.Sprintf("line %d: ", issue.Line)
	} else if issue.Field != "" {
		loc = issue.Field + ": "
	}
	fmt.Printf("%s %s%s\n", prefix, loc, issue.Message)
	if issue.Suggestion != "" {
		fmt.Printf("      %s\n", issue.Suggestion)
	}
}
