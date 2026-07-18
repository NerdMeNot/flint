// Package dbkit provides PostgreSQL connection pool setup and migration utilities.
// Uses pgx for database access with plain SQL — no ORM.
package dbkit

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// defaultMaxConns is a floor for the pool size. pgx defaults to
// max(4, NumCPU), which starves a control-plane node on a small box: one
// connection is pinned to the engine's LISTEN loop, the outbox fan-out can take
// up to 8 concurrently, and the engine + fleet ticks and agent gRPC calls each
// need one — 4 total blocks (waits, not errors), surfacing as latency stalls
// that are hard to diagnose. Floor the pool at a value that covers that steady
// state; a bigger box keeps its higher NumCPU default, and an explicit config
// value always wins.
const defaultMaxConns int32 = 20

// NewPool creates a pgx connection pool from the given config.
func NewPool(ctx context.Context, cfg Config) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("dbkit: failed to parse config: %w", err)
	}

	switch {
	case cfg.MaxConns > 0:
		poolCfg.MaxConns = cfg.MaxConns // explicit config wins
	case poolCfg.MaxConns < defaultMaxConns:
		poolCfg.MaxConns = defaultMaxConns // floor an unconfigured small node
	}
	if cfg.MinConns > 0 {
		poolCfg.MinConns = cfg.MinConns
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("dbkit: failed to connect: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("dbkit: failed to ping: %w", err)
	}

	return pool, nil
}
