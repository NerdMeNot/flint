package dbkit

import (
	"context"
	"os"
	"testing"
)

// TestNewPool_FloorsMaxConns verifies the pool-size floor: an unconfigured
// deployment must not run on pgx's max(4, NumCPU) default (which starves the
// LISTEN loop + outbox fan-out on a small node), and an explicit config value
// always wins — even below the floor, since it's a deliberate choice.
func TestNewPool_FloorsMaxConns(t *testing.T) {
	if os.Getenv("FLINT_TEST_DSN") == "" {
		t.Skip("no FLINT_TEST_DSN; pool creation needs a reachable Postgres")
	}
	cfg := Config{Host: "localhost", Port: 5432, Database: "flint", User: "flint", Password: "flint"}

	unconfigured, err := NewPool(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewPool (unconfigured): %v", err)
	}
	defer unconfigured.Close()
	if got := unconfigured.Config().MaxConns; got < defaultMaxConns {
		t.Fatalf("unconfigured MaxConns = %d, want >= floor %d", got, defaultMaxConns)
	}

	cfg.MaxConns = 3 // explicit, below the floor
	explicit, err := NewPool(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewPool (explicit): %v", err)
	}
	defer explicit.Close()
	if got := explicit.Config().MaxConns; got != 3 {
		t.Fatalf("explicit MaxConns = %d, want 3 (config wins over the floor)", got)
	}
}
