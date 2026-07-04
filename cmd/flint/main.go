package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/NerdMeNot/flint/internal/cliauth"
	"github.com/NerdMeNot/flint/internal/products/ci"
	"github.com/NerdMeNot/flint/internal/tui"
	"github.com/NerdMeNot/flint/pkg/pipeline"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
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
		Long: `Flint is a Kubernetes-native CI platform on a Postgres-backed
durable engine. Durable by default, K8s-native, and not Jenkins.`,
		Version: fmt.Sprintf("%s (%s)", version, commit),
		RunE:    runTUI,
	}

	root.PersistentFlags().StringVar(&serverURL, "server", "", "Flint server URL (default: $FLINT_SERVER_URL or http://localhost:8080)")
	root.Flags().StringVar(&runID, "run", "", "jump to run detail by ID")
	root.Flags().StringVar(&project, "project", "", "jump to project by name")
	root.Flags().BoolVar(&gates, "gates", false, "show pending gate approvals")

	root.AddCommand(loginCmd())
	root.AddCommand(logoutCmd())
	root.AddCommand(initPipelineCmd())
	root.AddCommand(validateCmd())
	root.AddCommand(runCmd())
	root.AddCommand(logsCmd())
	root.AddCommand(adminCmd())
	root.AddCommand(devCmd())
	root.AddCommand(runnerCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func runTUI(cmd *cobra.Command, args []string) error {
	// Authenticated by default: load stored credentials (flint login).
	// A --server override without stored credentials falls back to an
	// unauthenticated client — useful only against demo/dev servers.
	var client *tui.Client
	if creds, err := cliauth.Load(); err == nil {
		if serverURL != "" {
			creds.ServerURL = strings.TrimRight(serverURL, "/")
		}
		client = tui.NewAuthedClient(creds)
	} else {
		base := serverURL
		if base == "" {
			base = os.Getenv("FLINT_SERVER_URL")
		}
		if base == "" {
			return fmt.Errorf("%w (or pass --server for an unauthenticated demo server)", err)
		}
		client = tui.NewClient(base)
	}

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

				// Dialect detection: the jobs→steps model (canonical, what the
				// server runs) vs the legacy flat-steps model.
				if isJobsDialect(data) {
					if !validateCIFile(f, data, envFlag) {
						hasErrors = true
					}
					continue
				}
				if !validateLegacyFile(f, data, envFlag) {
					hasErrors = true
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

// cmd0Ctx is the CLI's background context (validation has no cancellation).
func cmd0Ctx() context.Context { return context.Background() }

// isJobsDialect probes the YAML top level for a jobs: mapping.
func isJobsDialect(data []byte) bool {
	var probe struct {
		Jobs    map[string]yaml.Node `yaml:"jobs"`
		Extends string               `yaml:"extends"`
	}
	if yaml.Unmarshal(data, &probe) != nil {
		// Syntax errors are reported by the dialect validator; prefer the
		// canonical dialect for them.
		return true
	}
	return len(probe.Jobs) > 0 || probe.Extends != ""
}

// validateCIFile validates a jobs→steps pipeline — the dialect the server
// runs. Returns true when the file is valid.
func validateCIFile(f string, data []byte, envFlag string) bool {
	p, parseErr := ci.Parse(data)
	if parseErr != nil {
		// Parse already ran full validation; re-run the rich validator on a
		// lenient decode to print EVERY issue, falling back to the raw error
		// for syntax/strict-decode failures.
		var lenient ci.Pipeline
		if yaml.Unmarshal(data, &lenient) == nil {
			r := lenient.ValidateDetailed()
			if !r.Valid() {
				fmt.Printf("  ✗ %s — %d error(s)\n", f, len(r.Errors()))
				for _, issue := range r.Errors() {
					printIssue(issue)
				}
				for _, issue := range r.Warnings() {
					printIssue(issue)
				}
				// Strict-decode errors (unknown fields) don't show up in the
				// rich pass — surface them too when present.
				if strings.Contains(parseErr.Error(), "not found in type") {
					fmt.Printf("    ✗ %v\n", parseErr)
				}
				return false
			}
		}
		fmt.Printf("  ✗ %s — %v\n", f, parseErr)
		return false
	}

	// Resolve module references offline: built-ins + in-repo ./ files (the
	// repo root is the parent of the pipeline directory).
	root, _ := filepath.Abs(filepath.Join(filepath.Dir(f), ".."))
	resolved, resErr := ci.ResolveModules(cmd0Ctx(), p, &ci.FileResolver{Root: root})
	if resErr != nil {
		fmt.Printf("  ✗ %s — %v\n", f, resErr)
		return false
	}
	p = resolved

	r := p.ValidateDetailed()
	waves, compileErr := ci.Compile(p, envFlag)
	if compileErr != nil {
		fmt.Printf("  ✗ %s — %v\n", f, compileErr)
		return false
	}

	jobs := 0
	for _, w := range waves {
		jobs += len(w)
	}
	envAware := ""
	if len(p.Environments) > 0 {
		envAware = " [env-aware]"
	}
	fmt.Printf("  ✓ %s — %d jobs, %d waves%s\n", f, jobs, len(waves), envAware)
	for _, issue := range r.Warnings() {
		printIssue(issue)
	}

	// Environment preview: which jobs run for --env.
	if envFlag != "" {
		fmt.Printf("\n    Preview: %s → %s\n", f, envFlag)
		fmt.Printf("    Jobs:\n")
		compiled := map[string]bool{}
		for _, w := range waves {
			for _, s := range w {
				compiled[s.Name] = true
				fmt.Printf("      ✓ %s\n", s.Name)
			}
		}
		for _, name := range sortedNames(p.Jobs) {
			if !compiled[name] && !hasMatrixVariant(compiled, name) {
				fmt.Printf("      ○ %s — filtered out (environments: %s)\n",
					name, strings.Join(p.Jobs[name].Environments, ", "))
			}
		}
	}
	return true
}

func sortedNames(jobs map[string]ci.Job) []string {
	names := make([]string, 0, len(jobs))
	for n := range jobs {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func hasMatrixVariant(compiled map[string]bool, base string) bool {
	for name := range compiled {
		if strings.HasPrefix(name, base+"::") {
			return true
		}
	}
	return false
}

// validateLegacyFile validates a flat-steps pipeline (deprecated dialect).
// Returns true when the file is valid.
func validateLegacyFile(f string, data []byte, envFlag string) bool {
	fmt.Printf("  ⚠ %s uses the legacy flat-steps dialect — migrate to jobs: (see docs/design/pipeline-spec.md)\n", f)

	result := pipeline.Validate(data, pipeline.ValidateOptions{})
	if !result.Valid() {
		fmt.Printf("  ✗ %s — %d error(s)\n", f, len(result.Errors()))
		for _, issue := range result.Errors() {
			printIssue(issue)
		}
		for _, issue := range result.Warnings() {
			printIssue(issue)
		}
		return false
	}

	p, _ := pipeline.Parse(data)
	waves, _ := pipeline.ResolveDag(p)

	envAware := ""
	if pipeline.IsEnvironmentAware(p) {
		envAware = " [env-aware]"
	}
	fmt.Printf("  ✓ %s — %d steps, %d waves%s\n", f, len(p.Steps), len(waves), envAware)
	for _, issue := range result.Warnings() {
		printIssue(issue)
	}

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
	return true
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
