package server

import (
	"sync"

	"github.com/NerdMeNot/flint/pkg/logsink"
)

// LogStream is the interface for real-time log broadcasting to SSE clients.
// *logBroadcaster implements it; tests can mock it.
type LogStream interface {
	Subscribe(runID, stepName string) chan []logsink.LogLine
	Unsubscribe(runID, stepName string, ch chan []logsink.LogLine)
	Publish(runID, stepName string, lines []logsink.LogLine)
}

// logBroadcaster fans out log lines from agent ingestion to SSE subscribers.
// Zero subscribers = zero overhead (Publish is a no-op).
type logBroadcaster struct {
	mu   sync.Mutex
	subs map[string][]*logSubscriber // key: "runID:stepName"
}

type logSubscriber struct {
	ch     chan []logsink.LogLine
	closed bool
}

// NewlogBroadcaster creates a new broadcaster.
// NewLogStream creates a new log broadcaster that implements LogStream.
func NewLogStream() LogStream {
	return &logBroadcaster{
		subs: make(map[string][]*logSubscriber),
	}
}

func broadcastKey(runID, stepName string) string {
	return runID + ":" + stepName
}

// Subscribe returns a channel that receives log line batches for the given
// step. The caller must call Unsubscribe when done.
func (b *logBroadcaster) Subscribe(runID, stepName string) chan []logsink.LogLine {
	key := broadcastKey(runID, stepName)
	sub := &logSubscriber{
		ch: make(chan []logsink.LogLine, 64),
	}

	b.mu.Lock()
	b.subs[key] = append(b.subs[key], sub)
	b.mu.Unlock()

	return sub.ch
}

// Unsubscribe removes a subscriber channel. Safe to call multiple times.
// The channel is NOT closed here — it is simply marked as dead and removed
// from the map. This prevents send-on-closed-channel panics in Publish.
func (b *logBroadcaster) Unsubscribe(runID, stepName string, ch chan []logsink.LogLine) {
	key := broadcastKey(runID, stepName)

	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.subs[key]
	for i, s := range subs {
		if s.ch == ch {
			s.closed = true
			b.subs[key] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	if len(b.subs[key]) == 0 {
		delete(b.subs, key)
	}
}

// Publish sends a batch of log lines to all subscribers for this step.
// Non-blocking: if a subscriber's channel is full, the batch is dropped for
// that subscriber (slow consumer protection).
//
// Holds the lock for the entire publish to prevent racing with Unsubscribe.
func (b *logBroadcaster) Publish(runID, stepName string, lines []logsink.LogLine) {
	key := broadcastKey(runID, stepName)

	b.mu.Lock()
	subs := b.subs[key]
	for _, s := range subs {
		if s.closed {
			continue
		}
		select {
		case s.ch <- lines:
		default:
		}
	}
	b.mu.Unlock()
}
