package boot

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/dbkit"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/NerdMeNot/flint/internal/platform/config"
)

// RunIdPSync runs the IdP session-validation / group-sync loop as a standalone
// process (`flint server --mode sync`). In all/api modes the same loop runs as
// a goroutine inside RunServer; this mode exists for deployments that want the
// sync workload isolated.
func RunIdPSync(ctx context.Context, cfg *config.Config) error {
	pool, err := dbkit.NewPool(ctx, dbkit.Config{
		Host:     cfg.Database.Host,
		Port:     cfg.Database.PortOrDefault(),
		Database: cfg.Database.Database,
		User:     cfg.Database.User,
		Password: cfg.Database.Password,
		SSLMode:  cfg.Database.SSLMode,
	})
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	// OIDC provider. Sourced from the DB first (so it matches the server's
	// API-managed config, including claim mapping), falling back to the file.
	masterKey, masterKeyErr := cfg.Encryption.DecodeMasterKey()
	oidcProvider, err := buildOIDCProvider(ctx, cfg, db.New(pool), masterKey, masterKeyErr == nil)
	if err != nil {
		return err
	}
	if oidcProvider == nil {
		log.Warn().Msg("OIDC not configured — sync will only clean up expired sessions")
	}

	// Casbin enforcer for policy regeneration after group sync.
	enforcer, err := auth.NewEnforcer(pool)
	if err != nil {
		return fmt.Errorf("Casbin enforcer: %w", err)
	}

	log.Info().Msg("IdP sync ready")
	return auth.RunIdPSyncLoop(ctx, idpSyncConfig(cfg), pool, oidcProvider, masterKey, enforcer)
}

// idpSyncConfig maps the config file's sync block to the auth loop's config.
func idpSyncConfig(cfg *config.Config) auth.IdPSyncConfig {
	return auth.IdPSyncConfig{
		Interval:   parseDurationOr(cfg.Sync.Interval, 15*time.Minute),
		BatchSize:  50,
		StaleAfter: time.Hour,
	}
}

func parseDurationOr(s string, fallback time.Duration) time.Duration {
	if s == "" {
		return fallback
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return fallback
	}
	return d
}
