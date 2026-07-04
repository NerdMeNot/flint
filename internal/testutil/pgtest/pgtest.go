// Package pgtest provisions a throwaway local Postgres for integration tests
// — the same pattern the engine's TestMain uses, shared so fleet/agentgrpc
// suites don't each reimplement it. Uses the local initdb/pg_ctl binaries
// (e.g. Homebrew's postgresql); callers skip when none are available and
// FLINT_TEST_DSN is unset.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5"
)

// DSN resolves a test database: FLINT_TEST_DSN when set (CI service
// container), otherwise the autoDSN a Main-started local Postgres exported.
// Skips the test when neither exists or -short is set.
func DSN(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test skipped in -short mode")
	}
	if dsn := os.Getenv("FLINT_TEST_DSN"); dsn != "" {
		return dsn
	}
	t.Skip("no FLINT_TEST_DSN and no local postgres (initdb/pg_ctl) available")
	return ""
}

// Main wraps testing.M: it starts one throwaway Postgres for the package's
// integration tests (unless -short or FLINT_TEST_DSN pre-set) and exports
// FLINT_TEST_DSN for DSN(). Call from the package's TestMain.
//
// When FLINT_TEST_DSN is pre-set (a CI service container), the server is
// SHARED across `go test` invocations — the plain and -race suites run
// back-to-back against it, and tests that count rows would see the previous
// suite's data. So Main carves a scratch database per invocation and drops
// it afterward; tests still see a fresh database, matching the local
// throwaway-instance behaviour.
func Main(m *testing.M) int {
	var stop func()
	switch {
	case shortMode():
	case os.Getenv("FLINT_TEST_DSN") != "":
		if dsn, drop, err := createScratchDB(os.Getenv("FLINT_TEST_DSN")); err != nil {
			// Degrades to the shared database — functional, less isolated.
			fmt.Fprintf(os.Stderr, "pgtest: scratch database unavailable (%v); using FLINT_TEST_DSN as-is\n", err)
		} else {
			stop = drop
			_ = os.Setenv("FLINT_TEST_DSN", dsn)
		}
	default:
		dsn, stopFn, err := StartLocalPostgres()
		if err != nil {
			fmt.Fprintf(os.Stderr, "integration tests: %v; set FLINT_TEST_DSN to run them\n", err)
		} else {
			stop = stopFn
			_ = os.Setenv("FLINT_TEST_DSN", dsn)
		}
	}
	code := m.Run()
	if stop != nil {
		stop()
	}
	return code
}

// createScratchDB creates a uniquely named database on the server baseDSN
// points at and returns a DSN for it plus a drop function.
func createScratchDB(baseDSN string) (string, func(), error) {
	ctx := context.Background()
	u, err := url.Parse(baseDSN)
	if err != nil {
		return "", nil, err
	}
	nonce := make([]byte, 4)
	if _, err := rand.Read(nonce); err != nil {
		return "", nil, err
	}
	name := "flint_test_" + hex.EncodeToString(nonce)

	conn, err := pgx.Connect(ctx, baseDSN)
	if err != nil {
		return "", nil, err
	}
	_, err = conn.Exec(ctx, "CREATE DATABASE "+name)
	_ = conn.Close(ctx)
	if err != nil {
		return "", nil, err
	}

	u.Path = "/" + name
	drop := func() {
		conn, err := pgx.Connect(ctx, baseDSN)
		if err != nil {
			return
		}
		_, _ = conn.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
		_ = conn.Close(ctx)
	}
	return u.String(), drop, nil
}

// shortMode detects -short without flag.Parse side effects (the caller's
// TestMain may not have parsed flags yet).
func shortMode() bool {
	for _, arg := range os.Args[1:] {
		if arg == "-test.short" || arg == "-test.short=true" {
			return true
		}
	}
	return false
}

// StartLocalPostgres initialises and starts a throwaway Postgres in a temp
// dir on a free port, returning its DSN and a stop function. pg_ctl start
// blocks until the server is ready, so the DSN is usable on return.
func StartLocalPostgres() (dsn string, stop func(), err error) {
	initdb, err := exec.LookPath("initdb")
	if err != nil {
		return "", nil, fmt.Errorf("initdb not found on PATH")
	}
	bin := filepath.Dir(initdb)
	pgCtl := filepath.Join(bin, "pg_ctl")
	createDB := filepath.Join(bin, "createdb")

	dataDir, err := os.MkdirTemp("", "flint-pgtest-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dataDir) }

	if out, err := exec.Command(initdb, "-D", dataDir, "-U", "flint", "--auth=trust").CombinedOutput(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("initdb: %v: %s", err, out)
	}

	port, err := freePort()
	if err != nil {
		cleanup()
		return "", nil, err
	}

	logFile := filepath.Join(dataDir, "server.log")
	if out, err := exec.Command(pgCtl, "-D", dataDir, "-o", fmt.Sprintf("-p %d", port), "-l", logFile, "start").CombinedOutput(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("pg_ctl start: %v: %s", err, out)
	}

	stop = func() {
		_ = exec.Command(pgCtl, "-D", dataDir, "stop").Run()
		cleanup()
	}

	if out, err := exec.Command(createDB, "-h", "localhost", "-p", fmt.Sprintf("%d", port), "-U", "flint", "flint").CombinedOutput(); err != nil {
		stop()
		return "", nil, fmt.Errorf("createdb: %v: %s", err, out)
	}

	return fmt.Sprintf("postgres://flint@localhost:%d/flint?sslmode=disable", port), stop, nil
}

// freePort asks the OS for an unused TCP port.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close() //nolint:errcheck
	return l.Addr().(*net.TCPAddr).Port, nil
}
