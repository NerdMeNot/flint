package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/casbin/casbin/v3/persist"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// casbinChannel is the Postgres NOTIFY channel carrying "the policy table
// changed, reload" between replicas.
const casbinChannel = "flint_casbin"

// notifyDebounce is how long a burst of policy edits is allowed to accumulate
// before peers are told about it.
//
// Casbin fires the watcher once per rule, and a single login regenerates every
// rule for that subject — tens of them. Sent one-for-one, each would make every
// other replica re-read the whole policy table, so a busy login hour would
// spend itself reloading policy. Both directions are coalesced instead: many
// edits become one NOTIFY, and a burst of NOTIFYs becomes one reload. The cost
// is that a policy change reaches peers a fraction of a second later, which for
// a permission grant is not a meaningful delay.
const notifyDebounce = 100 * time.Millisecond

// PgWatcher is a persist.Watcher over Postgres LISTEN/NOTIFY.
//
// Without a watcher each replica's enforcer is a private snapshot taken at
// boot: a role granted through replica A is invisible to replica B until B
// restarts, and a revocation is worse — the user keeps the access. The engine
// already uses LISTEN/NOTIFY for exactly this shape of problem, so the policy
// layer uses it too rather than adding a second mechanism.
//
// The instance ignores its own notifications: the writer already has the change
// in memory, and reloading on every local write would turn each policy edit
// into a full table read.
type PgWatcher struct {
	pool   db.Pool
	self   string
	cancel context.CancelFunc

	mu       sync.Mutex
	callback func(string)
	closed   bool

	sendPending bool
	sendTimer   *time.Timer
	recvPending bool
	recvTimer   *time.Timer
}

var _ persist.Watcher = (*PgWatcher)(nil)

// NewPgWatcher starts a listener on the casbin channel. A pool that cannot
// Acquire a dedicated connection (test doubles, non-pgxpool implementations)
// yields a watcher that still coalesces and sends, but does not receive:
// single-process installs and tests do not need cross-replica propagation, and
// failing boot over it would be worse than running without it. The degradation
// is logged, loudly — a silently inert watcher would look exactly like a
// working one.
func NewPgWatcher(ctx context.Context, pool db.Pool) (*PgWatcher, error) {
	self, err := randomID()
	if err != nil {
		return nil, fmt.Errorf("generating watcher id: %w", err)
	}

	lctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	w := &PgWatcher{pool: pool, self: self, cancel: cancel}

	pgPool, ok := pool.(*pgxpool.Pool)
	if !ok {
		cancel()
		log.Warn().Msg("casbin: pool does not support Acquire, policy changes will not propagate between replicas")
		return w, nil
	}

	go w.listen(lctx, pgPool)
	return w, nil
}

// SetUpdateCallback registers the reload hook casbin invokes on a peer update.
func (w *PgWatcher) SetUpdateCallback(cb func(string)) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.callback = cb
	return nil
}

// Update records that this instance changed the policy. The actual NOTIFY is
// coalesced, so this returns before peers have been told.
//
// It reports no error: the send happens later, and a failed NOTIFY is a
// propagation delay rather than a lost write — the rules are already committed
// by the adapter, and peers converge on the next change or restart. Errors are
// logged where they occur.
func (w *PgWatcher) Update() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.sendPending = true
	if w.sendTimer == nil {
		w.sendTimer = time.AfterFunc(notifyDebounce, w.flushSend)
	}
	return nil
}

func (w *PgWatcher) flushSend() {
	w.mu.Lock()
	w.sendTimer = nil
	if !w.sendPending || w.closed {
		w.mu.Unlock()
		return
	}
	w.sendPending = false
	self := w.self
	w.mu.Unlock()

	if _, err := w.pool.Exec(context.Background(),
		"SELECT pg_notify($1, $2)", casbinChannel, self); err != nil {
		log.Error().Err(err).Msg("casbin: notifying peers of a policy change failed; they will converge on the next change")
	}
}

func (w *PgWatcher) flushRecv() {
	w.mu.Lock()
	w.recvTimer = nil
	if !w.recvPending || w.closed {
		w.mu.Unlock()
		return
	}
	w.recvPending = false
	cb := w.callback
	w.mu.Unlock()

	if cb != nil {
		cb("")
	}
}

// Close stops the listener and drops any pending notifications.
func (w *PgWatcher) Close() {
	w.mu.Lock()
	w.closed = true
	if w.sendTimer != nil {
		w.sendTimer.Stop()
		w.sendTimer = nil
	}
	if w.recvTimer != nil {
		w.recvTimer.Stop()
		w.recvTimer = nil
	}
	w.mu.Unlock()
	w.cancel()
}

// randomID identifies this process on the notify channel so it can skip its
// own broadcasts.
func randomID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (w *PgWatcher) listen(ctx context.Context, pool *pgxpool.Pool) {
	for {
		if ctx.Err() != nil {
			return
		}

		conn, err := pool.Acquire(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			time.Sleep(time.Second)
			continue
		}

		if _, err := conn.Exec(ctx, "LISTEN "+casbinChannel); err != nil {
			conn.Release()
			if ctx.Err() != nil {
				return
			}
			time.Sleep(time.Second)
			continue
		}

		for {
			n, err := conn.Conn().WaitForNotification(ctx)
			if err != nil {
				conn.Release()
				if ctx.Err() != nil {
					return
				}
				break // reconnect
			}
			if n.Payload == w.self {
				continue // our own write; already in memory
			}
			w.scheduleReload()
		}
	}
}

// scheduleReload coalesces a burst of peer notifications into a single policy
// reload.
func (w *PgWatcher) scheduleReload() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return
	}
	w.recvPending = true
	if w.recvTimer == nil {
		w.recvTimer = time.AfterFunc(notifyDebounce, w.flushRecv)
	}
}
