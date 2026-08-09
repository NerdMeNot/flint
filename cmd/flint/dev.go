package main

import (
	"context"
	"fmt"
	"os"
	"runtime"

	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/dbkit"
	"github.com/NerdMeNot/flint/internal/core/fleet"
	"github.com/NerdMeNot/flint/internal/devseed"
	"github.com/NerdMeNot/flint/internal/platform/config"
)

// quietGoose silences goose's per-migration chatter ("OK …", "no migrations to
// run") during seeding; real failures still surface (RunMigrations returns them).
type quietGoose struct{}

func (quietGoose) Printf(string, ...interface{}) {}
func (quietGoose) Fatalf(format string, v ...interface{}) {
	fmt.Fprintf(os.Stderr, format, v...)
}

func devCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev",
		Short: "Local development helpers (not for production use)",
	}
	cmd.AddCommand(seedCmd())
	cmd.AddCommand(migrateCmd())
	cmd.AddCommand(joinTokenCmd())
	cmd.AddCommand(fleetDemoCmd())
	return cmd
}

// joinTokenCmd mints (or rotates) a static pool's agent join token straight in
// the DB — the dev shortcut behind `task dev-agent`. Production installs mint
// via POST /api/v1/runners/:name/token instead. Creates the pool when absent
// so a bare dev-local database is one command away from a joinable fleet.
func joinTokenCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "join-token [pool]",
		Short: "Mint a static pool's agent join token (dev shortcut; creates the pool if missing)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			poolName := "standard"
			if len(args) == 1 {
				poolName = args[0]
			}
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

			if _, err := q.GetMachinePool(ctx, poolName); err != nil {
				if err := q.UpsertMachinePool(ctx, db.UpsertMachinePoolParams{
					Name: poolName, Provider: "static", Arch: runtime.GOARCH,
					Cpu: "4", Memory: "8Gi", CapacityType: "on_demand",
					Objective: "balanced", MaxMachines: 10, IdleTtlSeconds: 900,
				}); err != nil {
					return fmt.Errorf("creating pool %s: %w", poolName, err)
				}
				if n, err := q.CountDefaultMachinePools(ctx); err == nil && n == 0 {
					_ = q.SetDefaultMachinePool(ctx, poolName)
				}
				fmt.Fprintf(os.Stderr, "created static pool %q\n", poolName)
			}

			token, hash, err := fleet.MintToken()
			if err != nil {
				return err
			}
			if err := q.SetPoolJoinTokenHash(ctx, db.SetPoolJoinTokenHashParams{
				Name: poolName, JoinTokenHash: &hash,
			}); err != nil {
				return err
			}
			// Token on stdout only, so `TOKEN=$(flint dev join-token)` works.
			fmt.Println(token)
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to config file")
	return cmd
}

// migrateCmd applies/rolls back/inspects migrations via the embedded migration FS
// and the vendored goose library. This works under `-mod=vendor`, unlike the
// standalone goose CLI (`go run github.com/pressly/goose/v3/cmd/goose ...`), whose
// import lookup is disabled when a vendor directory is present.
func migrateCmd() *cobra.Command {
	var dsn, configPath string

	// resolveDSN prefers an explicit --dsn, then $DATABASE_URL, then a config file.
	resolveDSN := func() (string, error) {
		if dsn != "" {
			return dsn, nil
		}
		if env := os.Getenv("DATABASE_URL"); env != "" {
			return env, nil
		}
		cfg, err := config.Load(configPath)
		if err != nil {
			return "", fmt.Errorf("loading config: %w", err)
		}
		dbCfg := dbkit.Config{
			Host:     cfg.Database.Host,
			Port:     cfg.Database.PortOrDefault(),
			Database: cfg.Database.Database,
			User:     cfg.Database.User,
			Password: cfg.Database.Password,
			SSLMode:  cfg.Database.SSLMode,
		}
		return dbCfg.DSN(), nil
	}

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply, roll back, or inspect database migrations (vendored goose)",
		Long: `Runs migrations from the embedded migration filesystem via the
vendored goose library, so it works under -mod=vendor (unlike the standalone
goose CLI). DSN resolution: --dsn, then $DATABASE_URL, then --config.`,
	}
	cmd.PersistentFlags().StringVar(&dsn, "dsn", "", "database connection string (falls back to $DATABASE_URL, then --config)")
	cmd.PersistentFlags().StringVar(&configPath, "config", "config.local.yaml", "config file to derive the DSN when --dsn/$DATABASE_URL are unset")

	cmd.AddCommand(&cobra.Command{
		Use:   "up",
		Short: "Apply all pending migrations",
		RunE: func(*cobra.Command, []string) error {
			d, err := resolveDSN()
			if err != nil {
				return err
			}
			return dbkit.RunMigrations(d, dbkit.Migrations, "migrations")
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "down",
		Short: "Roll back the most recent migration",
		RunE: func(*cobra.Command, []string) error {
			d, err := resolveDSN()
			if err != nil {
				return err
			}
			return dbkit.RollbackMigration(d, dbkit.Migrations, "migrations")
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show migration status",
		RunE: func(*cobra.Command, []string) error {
			d, err := resolveDSN()
			if err != nil {
				return err
			}
			return dbkit.MigrationStatus(d, dbkit.Migrations, "migrations")
		},
	})
	return cmd
}

func seedCmd() *cobra.Command {
	var configPath string

	cmd := &cobra.Command{
		Use:   "seed",
		Short: "Seed the local database with realistic CI data for the sim stack",
		Long: `Populates Postgres with projects and runs built through the real
product paths, so the worker (FLINT_EXECUTOR=sim) drives them to completion.
Idempotent: reseeds replace prior seed data. Intended for the local sim stack
(task dev-sim) — never run against a real database.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()

			// Quiet the noise: the engine logs an INFO line per triggered run and
			// goose logs each migration. The seeder prints its own clean summary, so
			// drop everything below WARN.
			zerolog.SetGlobalLevel(zerolog.WarnLevel)
			goose.SetLogger(quietGoose{})

			cfg, err := config.Load(configPath)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}

			dbCfg := dbkit.Config{
				Host:     cfg.Database.Host,
				Port:     cfg.Database.PortOrDefault(),
				Database: cfg.Database.Database,
				User:     cfg.Database.User,
				Password: cfg.Database.Password,
				SSLMode:  cfg.Database.SSLMode,
			}

			// Apply migrations via the embedded goose FS (the vendored library) so
			// the sim stack doesn't need the external goose binary, which can't run
			// under `-mod=vendor`.
			if err := dbkit.RunMigrations(dbCfg.DSN(), dbkit.Migrations, "migrations"); err != nil {
				return fmt.Errorf("migrate: %w", err)
			}

			pool, err := dbkit.NewPool(ctx, dbCfg)
			if err != nil {
				return fmt.Errorf("database: %w", err)
			}
			defer pool.Close()

			return devseed.Run(ctx, pool, []byte(cfg.Auth.JWT.Secret))
		},
	}

	cmd.Flags().StringVar(&configPath, "config", "", "path to config file")
	return cmd
}
