package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/dbkit"
	"github.com/NerdMeNot/flint/internal/core/runner"
	"github.com/NerdMeNot/flint/internal/core/runner/render"
	"github.com/NerdMeNot/flint/internal/platform/config"
	"github.com/spf13/cobra"
)

func runnerCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "runner",
		Short: "Inspect and render runner pools",
	}
	cmd.AddCommand(runnerRenderCmd())
	return cmd
}

func runnerRenderCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "render <pool>",
		Short: "Render a managed pool's Karpenter NodePool + EC2NodeClass (render-to-GitOps)",
		Long: `Prints the Karpenter manifests for a managed runner pool — apply these via
your GitOps (Argo) so Karpenter provisions the pool's nodes. Flint never writes to
the cluster directly. Reference pools have no manifests.`,
		Args: cobra.ExactArgs(1),
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

			row, err := db.New(pool).GetRunnerPool(ctx, args[0])
			if err != nil {
				return fmt.Errorf("runner pool %q not found", args[0])
			}
			if row.Mode != "managed" {
				return fmt.Errorf("pool %q is reference mode — only managed pools have manifests", args[0])
			}

			var ms runner.ManagedSpec
			if len(row.ManagedSpec) > 0 {
				_ = json.Unmarshal(row.ManagedSpec, &ms)
			}
			in := render.Input{
				Name: row.Name, Arch: row.Arch, Managed: ms,
				Prov: runner.ProvisioningProfile{
					Role:                  cfg.Provisioning.Role,
					SubnetSelector:        cfg.Provisioning.SubnetTags(),
					SecurityGroupSelector: cfg.Provisioning.SecurityGroupTags(),
					AMIFamily:             cfg.Provisioning.AMIFamily,
				},
			}
			if row.GpuVendor != nil && *row.GpuVendor != "" {
				model := ""
				if row.GpuModel != nil {
					model = *row.GpuModel
				}
				in.GPU = &runner.GPURequest{Vendor: *row.GpuVendor, Model: model, Count: int(row.GpuCount.Int32)}
			}
			m, err := render.Render(in)
			if err != nil {
				return err
			}
			fmt.Print(m.Combined())
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to config file")
	return cmd
}
