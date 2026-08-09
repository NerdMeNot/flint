package dbkit

import (
	"context"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
)

// configFromDSN builds a Config from FLINT_TEST_DSN. The test used to gate on
// that variable and then connect to a hardcoded localhost:5432 regardless of
// what it said — so it passed only where the test database happened to sit on
// the default port, and failed confusingly anywhere else.
func configFromDSN(t *testing.T, dsn string) Config {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parsing FLINT_TEST_DSN: %v", err)
	}
	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		host, portStr = u.Host, "5432"
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parsing port from FLINT_TEST_DSN: %v", err)
	}
	password, _ := u.User.Password()
	cfg := Config{
		Host:     host,
		Port:     port,
		Database: strings.TrimPrefix(u.Path, "/"),
		User:     u.User.Username(),
		Password: password,
		SSLMode:  u.Query().Get("sslmode"),
	}
	return cfg
}

// TestNewPool_FloorsMaxConns verifies the pool-size floor: an unconfigured
// deployment must not run on pgx's max(4, NumCPU) default (which starves the
// LISTEN loop + outbox fan-out on a small node), and an explicit config value
// always wins — even below the floor, since it's a deliberate choice.
func TestNewPool_FloorsMaxConns(t *testing.T) {
	dsn := os.Getenv("FLINT_TEST_DSN")
	if dsn == "" {
		t.Skip("no FLINT_TEST_DSN; pool creation needs a reachable Postgres")
	}
	cfg := configFromDSN(t, dsn)

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
