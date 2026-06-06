// Package main implements the flint-syncd daemon, which periodically validates
// active user sessions against the IdP and syncs group memberships.
//
// It shares the same configuration file as the server (config.yaml) and connects
// to the same Postgres database. Run alongside the server:
//
//	flint-syncd --config config.yaml
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/NerdMeNot/flint/internal/auth"
	"github.com/NerdMeNot/flint/internal/config"
	"github.com/NerdMeNot/flint/internal/dbkit"
	"github.com/NerdMeNot/flint/internal/observe"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var (
	version    = "dev"
	commit     = "unknown"
	configPath string
)

func main() {
	root := &cobra.Command{
		Use:     "flint-syncd",
		Short:   "Flint IdP sync daemon — validates sessions and syncs groups",
		Version: fmt.Sprintf("%s (%s)", version, commit),
		RunE:    run,
	}

	root.Flags().StringVar(&configPath, "config", "", "path to config file")

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	shutdown, err := observe.Init(ctx, observe.Config{
		ServiceName:    "flint-syncd",
		ServiceVersion: version,
		LogLevel:       "info",
	})
	if err != nil {
		log.Warn().Err(err).Msg("observability init failed (continuing)")
	}
	defer shutdown(ctx)

	log.Info().
		Str("version", version).
		Msg("flint-syncd starting")

	// Database.
	pool, err := dbkit.NewPool(ctx, dbkit.Config{
		Host:     cfg.Database.Host,
		Port:     cfg.Database.Port,
		Database: cfg.Database.Database,
		User:     cfg.Database.User,
		Password: cfg.Database.Password,
		SSLMode:  cfg.Database.SSLMode,
	})
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	// OIDC provider.
	var oidcProvider *auth.OIDCProvider
	if auth.OIDCConfigured(cfg.Auth.OIDC.IssuerURL, cfg.Auth.OIDC.ClientID) {
		oidcProvider, err = auth.NewOIDCProvider(ctx, auth.OIDCProviderConfig{
			IssuerURL:    cfg.Auth.OIDC.IssuerURL,
			ClientID:     cfg.Auth.OIDC.ClientID,
			ClientSecret: cfg.Auth.OIDC.ClientSecret,
		})
		if err != nil {
			return fmt.Errorf("OIDC provider: %w", err)
		}
		log.Info().Str("issuer", cfg.Auth.OIDC.IssuerURL).Msg("OIDC provider configured")
	} else {
		log.Warn().Msg("OIDC not configured — sync will only clean up expired sessions")
	}

	// Casbin enforcer for policy regeneration after group sync.
	enforcer, err := auth.NewEnforcer(pool)
	if err != nil {
		return fmt.Errorf("Casbin enforcer: %w", err)
	}

	// Run sync loop.
	syncCfg := auth.IdPSyncConfig{
		Interval:   parseDuration(cfg.Sync.Interval, 15*time.Minute),
		BatchSize:  50,
		StaleAfter: time.Hour,
	}

	log.Info().Msg("flint-syncd ready")

	return auth.RunIdPSyncLoop(ctx, syncCfg, pool, oidcProvider, nil, enforcer)
}

func parseDuration(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}
