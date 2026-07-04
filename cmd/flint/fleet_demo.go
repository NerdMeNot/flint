package main

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/dbkit"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/platform/config"
	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// fleetDemoCmd is the dogfooding trigger: it seeds a "local" compute provider
// and an elastic pool that draws from it, then starts a runner:local run so the
// running control plane's fleet loop provisions a local agent on demand
// (Quote → Create a flint-agent child → it registers → claims → runs the
// steps → idle scale-down → Destroy). Watch `task dev-fleet`'s server log to
// see the whole lifecycle, then the decision ledger / pool insights / run
// placement fill with real numbers.
//
// Requires a control plane running with the machine fleet (task dev-fleet, or
// `flint server --mode all`) and a built agent binary at bin/flint-agent.
func fleetDemoCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "fleet-demo",
		Short: "Seed a local elastic pool and trigger a run so the fleet provisions a local agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
			pool, err := dbkit.NewPool(ctx, dbkit.Config{
				Host: cfg.Database.Host, Port: cfg.Database.PortOrDefault(),
				Database: cfg.Database.Database, User: cfg.Database.User,
				Password: cfg.Database.Password, SSLMode: cfg.Database.SSLMode,
			})
			if err != nil {
				return fmt.Errorf("database: %w", err)
			}
			defer pool.Close()
			q := db.New(pool)

			// 1. The local compute provider (idempotent). Its Create spawns a
			// flint-agent child process on the hostshell runtime.
			providerCfg, _ := json.Marshal(map[string]any{
				"agentBinary":         "bin/flint-agent",
				"pricePerHourUsd":     0.12,
				"expectedBootSeconds": 3,
				"instanceType":        "local.host",
			})
			if _, err := q.UpsertComputeProvider(ctx, db.UpsertComputeProviderParams{
				Name: "local", ProviderType: "local", Config: providerCfg,
			}); err != nil {
				return fmt.Errorf("seed compute provider: %w", err)
			}

			// 2. An elastic pool drawing from it: zero warm (scale-to-zero), a
			// short idle TTL so scale-down is observable within a minute.
			if err := q.UpsertMachinePool(ctx, db.UpsertMachinePoolParams{
				Name: "local", Provider: "local", Arch: runtime.GOARCH, Cpu: "2", Memory: "2Gi",
				CapacityType: "on_demand", Objective: "balanced",
				MinWarm: 0, MaxMachines: 3, IdleTtlSeconds: 60,
			}); err != nil {
				return fmt.Errorf("seed pool: %w", err)
			}

			// 3. A throwaway org/project/run to hang the workflow on.
			orgID, projectID, runID := uuid.NewString(), uuid.NewString(), uuid.NewString()
			if err := seedDemoProject(ctx, pool, orgID, projectID); err != nil {
				return fmt.Errorf("seed project: %w", err)
			}
			file, ref, by := "demo.yaml", "main", "fleet-demo"
			if err := q.InsertPipelineRun(ctx, db.InsertPipelineRunParams{
				ID: runID, ProjectID: &projectID, OrgID: orgID,
				WorkflowFile: &file, TriggerType: "manual", TriggerRef: &ref, TriggeredBy: &by,
			}); err != nil {
				return fmt.Errorf("insert run: %w", err)
			}

			// 4. A two-step runner:local pipeline. hostshell runs the commands
			// on the host (no image), so keep them plain shell.
			p := &pipeline.Pipeline{Steps: []pipeline.Step{
				{Name: "build", Runner: "local", Run: pipeline.Cmd(`echo "building on $(hostname)"; emit artifact core-v1`)},
				{Name: "test", Runner: "local", Run: pipeline.Cmd(`echo "testing (needs.build.artifact via workspace affinity)"`), DependsOn: []string{"build"}},
			}}
			waves, err := pipeline.ResolveDag(p)
			if err != nil {
				return fmt.Errorf("resolve dag: %w", err)
			}

			eng := engine.New(pool, []byte(cfg.Auth.JWT.Secret))
			defer eng.Close()
			wfID, err := eng.StartWorkflowWithWaves(ctx, engine.StartWorkflowInput{
				RunID: runID, OrgID: orgID, ProjectID: projectID,
				Repo: "acme/fleet-demo", Ref: "main", CommitSHA: "demo1234",
				TriggerType: "manual", TriggeredBy: "fleet-demo",
				// The machine executor requires an image at dispatch time even
				// though the hostshell runtime (what localdev agents run) ignores
				// it — the executor can't know the agent's runtime. Nominal value.
				PipelineImage: "alpine:3.19",
			}, waves)
			if err != nil {
				return fmt.Errorf("start workflow: %w", err)
			}

			fmt.Printf("✓ triggered run %s (workflow %s) on pool 'local'\n", runID[:8], wfID[:8])
			fmt.Println("  Watch the server log: the fleet loop should provision a local agent,")
			fmt.Println("  which registers, claims both steps, and runs them; the machine idle-")
			fmt.Println("  reaps after ~60s. Then check the fleet UI (/fleet), the decision")
			fmt.Println("  ledger, and the pool's insights — they now have real data.")
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to config file")
	return cmd
}

// seedDemoProject inserts the minimal org/forge/workspace/project rows a run's
// foreign keys need.
func seedDemoProject(ctx context.Context, pool db.Pool, orgID, projectID string) error {
	forgeID, wsID := uuid.NewString(), uuid.NewString()
	stmts := []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO orgs (id, name, slug) VALUES ($1, $2, $3)`,
			[]any{orgID, "fleet-demo-" + orgID[:8], "fleet-demo-" + orgID[:8]}},
		{`INSERT INTO forge_connections (id, org_id, forge_type, display_name, webhook_secret, credentials_enc)
		  VALUES ($1, $2, 'github', 'fleet-demo', 'demo-secret', $3)`,
			[]any{forgeID, orgID, []byte{}}},
		{`INSERT INTO workspaces (id, org_id, name, slug, is_default) VALUES ($1, $2, 'Default', $3, true)`,
			[]any{wsID, orgID, "default-" + orgID[:8]}},
		{`INSERT INTO projects (id, org_id, forge_id, workspace_id, display_name, repo_path, repo_url)
		  VALUES ($1, $2, $3, $4, 'fleet-demo', 'acme/fleet-demo', 'https://example.com/acme/fleet-demo')`,
			[]any{projectID, orgID, forgeID, wsID}},
	}
	for _, s := range stmts {
		if _, err := pool.Exec(ctx, s.sql, s.args...); err != nil {
			return err
		}
	}
	return nil
}
