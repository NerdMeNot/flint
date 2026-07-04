package boot

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/internal/core/dbkit"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/platform/config"
)

// RunDispatch runs the engine loop as a standalone process (`flint server
// --mode dispatch`) for scale-out deployments that separate dispatch from the
// API. Single-binary installs don't need it: --mode all runs the same loop
// in-process via StartWorker.
func RunDispatch(ctx context.Context, cfg *config.Config) error {
	pool, err := dbkit.NewPool(ctx, dbkit.Config{
		Host:     cfg.Database.Host,
		Port:     cfg.Database.PortOrDefault(),
		Database: cfg.Database.Database,
		User:     cfg.Database.User,
		Password: cfg.Database.Password,
		SSLMode:  cfg.Database.SSLMode,
	})
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()

	// Engine. The JWT secret signs/verifies task tokens (must match the server;
	// never handed to step containers, unlike the internal token).
	eng := engine.New(pool, []byte(cfg.Auth.JWT.Secret))
	defer eng.Close()

	w, err := StartWorker(ctx, cfg, pool, eng)
	if err != nil {
		return fmt.Errorf("starting dispatch: %w", err)
	}

	log.Info().Msg("dispatch ready, starting engine loop")
	return w.Loop.Run(ctx)
}
