package engine_test

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// autoDSN is the connection string for a throwaway Postgres that TestMain spins
// up for integration tests when FLINT_TEST_DSN is not set. Empty if none.
var autoDSN string

// TestMain provisions a single throwaway Postgres for the whole package's
// integration tests so `go test` works with no manual database and no new
// dependency. It uses the local initdb/pg_ctl binaries (e.g. Homebrew's
// postgresql). Under -short, or when FLINT_TEST_DSN is set (CI service
// container), it does nothing. If the binaries aren't found, integration tests
// skip rather than fail.
func TestMain(m *testing.M) {
	flag.Parse()

	var stop func()
	if !testing.Short() && os.Getenv("FLINT_TEST_DSN") == "" {
		dsn, stopFn, err := startLocalPostgres()
		if err != nil {
			fmt.Fprintf(os.Stderr, "integration tests: %v; set FLINT_TEST_DSN to run them\n", err)
		} else {
			autoDSN = dsn
			stop = stopFn
			// Export so internal (package engine) tests resolve the same DB.
			_ = os.Setenv("FLINT_TEST_DSN", dsn)
		}
	}

	code := m.Run()
	if stop != nil {
		stop()
	}
	os.Exit(code)
}

// startLocalPostgres initialises and starts a throwaway Postgres in a temp dir
// on a free port, returning its DSN and a stop function. pg_ctl start blocks
// until the server is ready, so the DSN is usable on return.
func startLocalPostgres() (dsn string, stop func(), err error) {
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
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
