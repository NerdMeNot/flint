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
	return &cobra.Command{
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

				p, err := pipeline.Parse(data)
				if err != nil {
					fmt.Printf("  ✗ %s: %v\n", f, err)
					hasErrors = true
					continue
				}

				waves, err := pipeline.ResolveDag(p)
				if err != nil {
					fmt.Printf("  ✗ %s: %v\n", f, err)
					hasErrors = true
					continue
				}

				fmt.Printf("  ✓ %s — %q (%d steps, %d waves)\n", f, p.Name, len(p.Steps), len(waves))
			}

			if hasErrors {
				return fmt.Errorf("validation failed")
			}
			fmt.Printf("\n✓ All %d pipelines valid\n", len(files))
			return nil
		},
	}
}
