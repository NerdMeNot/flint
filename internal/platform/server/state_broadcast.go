package server

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/engine"
)

// StateEvent is a full run snapshot pushed to SSE subscribers. Full snapshots
// (not deltas) mean a dropped event is self-healing — the next one carries the
// complete current state. The shape mirrors GET /runs/:id/steps so the UI can
// reuse its decoder.
type StateEvent struct {
	Status   string             `json:"status"`
	Steps    []engine.StepState `json:"steps"`
	DagWaves json.RawMessage    `json:"dagWaves"`
}

// StateStream broadcasts run state changes to SSE subscribers, keyed by runID.
// Mirrors LogStream; *stateBroadcaster implements it, tests can mock it.
type StateStream interface {
	Subscribe(runID string) chan StateEvent
	Unsubscribe(runID string, ch chan StateEvent)
	Publish(runID string, ev StateEvent)
}

type stateBroadcaster struct {
	mu   sync.Mutex
	subs map[string][]*stateSubscriber
}

type stateSubscriber struct {
	ch     chan StateEvent
	closed bool
}

// NewStateStream creates a run-state broadcaster.
func NewStateStream() StateStream {
	return &stateBroadcaster{subs: make(map[string][]*stateSubscriber)}
}

func (b *stateBroadcaster) Subscribe(runID string) chan StateEvent {
	sub := &stateSubscriber{ch: make(chan StateEvent, 8)}
	b.mu.Lock()
	b.subs[runID] = append(b.subs[runID], sub)
	b.mu.Unlock()
	return sub.ch
}

// Unsubscribe marks the subscriber dead and removes it. The channel is not
// closed here (prevents send-on-closed races with Publish).
func (b *stateBroadcaster) Unsubscribe(runID string, ch chan StateEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.subs[runID]
	for i, s := range subs {
		if s.ch == ch {
			s.closed = true
			b.subs[runID] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	if len(b.subs[runID]) == 0 {
		delete(b.subs, runID)
	}
}

// Publish fans out a snapshot to all subscribers for the run. Non-blocking: a
// full subscriber channel drops the event (the next snapshot heals it).
func (b *stateBroadcaster) Publish(runID string, ev StateEvent) {
	b.mu.Lock()
	for _, s := range b.subs[runID] {
		if s.closed {
			continue
		}
		select {
		case s.ch <- ev:
		default:
		}
	}
	b.mu.Unlock()
}

// buildStateEvent assembles a snapshot for a workflow. Returns the run id (the
// SSE subscription key) and false if the workflow can't be queried.
func buildStateEvent(ctx context.Context, eng engine.Engine, q db.Querier, workflowID string) (string, StateEvent, bool) {
	st, err := eng.QueryWorkflow(ctx, workflowID)
	if err != nil {
		return "", StateEvent{}, false
	}
	dag, _ := q.GetWorkflowDAGWaves(ctx, workflowID)
	if len(dag) == 0 {
		dag = []byte("[]")
	}
	return st.RunID, StateEvent{Status: st.Status, Steps: st.Steps, DagWaves: dag}, true
}

// WireStateObserver hooks the engine so every committed state transition (step
// completion, cancellation) publishes a fresh snapshot to SSE subscribers. The
// engine emits only a workflowID — it never imports the server — keeping it
// product-agnostic. Cross-process transitions (e.g. gate signals advanced by the
// worker) are caught by the handler's poll fallback instead.
//
// onTerminal, when non-nil, additionally fires whenever a snapshot shows the
// run in a terminal state — the hook products use to report run completion to
// the forge (commit status / check run). Callers must dedupe: multiple
// transitions can commit after the run is already terminal.
func WireStateObserver(eng *engine.PgEngine, q db.Querier, bc StateStream, onTerminal func(ctx context.Context, runID, status string)) {
	if eng == nil || bc == nil {
		return
	}
	eng.SetStateObserver(func(ctx context.Context, workflowID string) {
		runID, ev, ok := buildStateEvent(ctx, eng, q, workflowID)
		if !ok {
			return
		}
		bc.Publish(runID, ev)
		if onTerminal != nil &&
			(ev.Status == "succeeded" || ev.Status == "failed" || ev.Status == "cancelled") {
			onTerminal(ctx, runID, ev.Status)
		}
	})
}
