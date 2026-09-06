package auth

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

// stubPool is a minimal db.Pool that records Exec calls. The shared mocks
// package imports auth, so it cannot be used from auth's own tests.
type stubPool struct {
	mu    sync.Mutex
	execs [][]any
	sql   []string
}

func (p *stubPool) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sql = append(p.sql, sql)
	p.execs = append(p.execs, args)
	return pgconn.CommandTag{}, nil
}
func (p *stubPool) Query(context.Context, string, ...any) (pgx.Rows, error) { return nil, nil }
func (p *stubPool) QueryRow(context.Context, string, ...any) pgx.Row        { return nil }
func (p *stubPool) Ping(context.Context) error                              { return nil }
func (p *stubPool) Begin(context.Context) (pgx.Tx, error)                   { return nil, nil }

func (p *stubPool) calls() ([][]any, []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([][]any(nil), p.execs...), append([]string(nil), p.sql...)
}

// A pool that cannot hand out a dedicated connection (test doubles, and any
// non-pgxpool implementation) must not fail boot. Cross-replica propagation is
// a scale-out concern; a single-process install refusing to start without it
// would be the worse outcome.
func TestPgWatcher_DoesNotFailWithoutADedicatedConn(t *testing.T) {
	w, err := NewPgWatcher(context.Background(), &stubPool{})
	require.NoError(t, err)
	require.NotNil(t, w)
	w.Close()
}

// Update eventually broadcasts on the notify channel so peers reload. The
// payload is this instance's id, which is how it later skips its own
// notification.
func TestPgWatcher_UpdateNotifiesPeers(t *testing.T) {
	p := &stubPool{}
	w, err := NewPgWatcher(context.Background(), p)
	require.NoError(t, err)
	defer w.Close()

	require.NoError(t, w.Update())

	require.Eventually(t, func() bool {
		execs, _ := p.calls()
		return len(execs) == 1
	}, 2*time.Second, 10*time.Millisecond)

	execs, sql := p.calls()
	require.Contains(t, sql[0], "pg_notify")
	require.Equal(t, casbinChannel, execs[0][0])
	require.Equal(t, w.self, execs[0][1], "payload must identify this instance")
}

// The important property: casbin calls the watcher once per rule, and a single
// login rewrites every rule for that subject. A burst must become one NOTIFY,
// not one per rule — otherwise every peer re-reads the whole policy table
// dozens of times per login.
func TestPgWatcher_CoalescesABurst(t *testing.T) {
	p := &stubPool{}
	w, err := NewPgWatcher(context.Background(), p)
	require.NoError(t, err)
	defer w.Close()

	for range 50 {
		require.NoError(t, w.Update())
	}

	require.Eventually(t, func() bool {
		execs, _ := p.calls()
		return len(execs) >= 1
	}, 2*time.Second, 10*time.Millisecond)

	// Give any straggler timer a chance to fire before asserting the count.
	time.Sleep(3 * notifyDebounce)
	execs, _ := p.calls()
	require.Len(t, execs, 1, "50 rule edits must produce exactly one NOTIFY")
}

// After Close, a pending notification is dropped rather than firing against a
// pool that may already be shutting down.
func TestPgWatcher_UpdateAfterCloseIsQuiet(t *testing.T) {
	p := &stubPool{}
	w, err := NewPgWatcher(context.Background(), p)
	require.NoError(t, err)
	w.Close()

	require.NoError(t, w.Update())

	time.Sleep(3 * notifyDebounce)
	execs, _ := p.calls()
	require.Empty(t, execs)
}

// A Close racing an in-flight debounce must not send either.
func TestPgWatcher_CloseCancelsAPendingNotify(t *testing.T) {
	p := &stubPool{}
	w, err := NewPgWatcher(context.Background(), p)
	require.NoError(t, err)

	require.NoError(t, w.Update())
	w.Close() // before the debounce elapses

	time.Sleep(3 * notifyDebounce)
	execs, _ := p.calls()
	require.Empty(t, execs)
}

// Each instance gets a distinct id, so two replicas can tell each other's
// broadcasts from their own.
func TestPgWatcher_InstanceIDsAreDistinct(t *testing.T) {
	a, err := NewPgWatcher(context.Background(), &stubPool{})
	require.NoError(t, err)
	defer a.Close()
	b, err := NewPgWatcher(context.Background(), &stubPool{})
	require.NoError(t, err)
	defer b.Close()

	require.NotEqual(t, a.self, b.self)
	require.NotEmpty(t, a.self)
}

// Inbound notifications coalesce the same way: a burst of peer updates must
// cause one reload, not one per notification.
func TestPgWatcher_CoalescesInboundReloads(t *testing.T) {
	w, err := NewPgWatcher(context.Background(), &stubPool{})
	require.NoError(t, err)
	defer w.Close()

	var reloads int
	var mu sync.Mutex
	require.NoError(t, w.SetUpdateCallback(func(string) {
		mu.Lock()
		reloads++
		mu.Unlock()
	}))

	for range 20 {
		w.scheduleReload()
	}

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return reloads >= 1
	}, 2*time.Second, 10*time.Millisecond)

	time.Sleep(3 * notifyDebounce)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, 1, reloads, "a burst of peer notifications must cause one reload")
}
