package server

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/cloudwego/hertz/pkg/app"
)

// handleStreamRunState streams a run's state (status + steps + dagWaves) to the
// browser over SSE: an initial snapshot, then a fresh snapshot whenever the
// engine observer fires or the 1s poll detects a change, then a terminal `done`
// event. Full snapshots (same shape as GET /runs/:id/steps) make dropped events
// self-healing. The poll is a belt-and-suspenders fallback so cross-process
// transitions (e.g. gate signals advanced by the worker) still surface.
// GET /api/v1/runs/:id/stream
func (s *Server) handleStreamRunState(ctx context.Context, c *app.RequestContext) {
	runID := c.Param("id")
	wfID, err := s.deps.Q.GetRunWorkflowID(ctx, runID)
	if err != nil || wfID == nil {
		apiNotFound(ctx, c, "run not found or no workflow")
		return
	}

	c.Response.Header.SetContentType("text/event-stream")
	c.Response.Header.Set("Cache-Control", "no-cache")
	c.Response.Header.Set("Connection", "keep-alive")
	c.Response.Header.Set("X-Accel-Buffering", "no")

	pr, pw := io.Pipe()
	c.Response.SetBodyStream(pr, -1)

	var sub chan StateEvent
	if s.deps.StateBroadcast != nil {
		sub = s.deps.StateBroadcast.Subscribe(runID)
	}

	go func() {
		defer pw.Close()
		if sub != nil {
			defer s.deps.StateBroadcast.Unsubscribe(runID, sub)
		}

		var lastSig string
		// send writes a snapshot only when it differs from the last one sent.
		send := func(ev StateEvent) bool {
			sig := ev.Status + "|" + stepsSignature(ev.Steps)
			if sig == lastSig {
				return true // unchanged, nothing to write, keep going
			}
			lastSig = sig
			return writeSSEEvent(pw, "state", ev) == nil
		}
		finish := func(status string) {
			_ = writeSSEEvent(pw, "done", map[string]string{"status": status})
		}

		// Initial snapshot.
		if _, ev, ok := buildStateEvent(ctx, s.deps.Engine, s.deps.Q, *wfID); ok {
			if !send(ev) {
				return
			}
			if isTerminalStatus(ev.Status) {
				finish(ev.Status)
				return
			}
		}

		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case ev, ok := <-sub:
				if !ok {
					return
				}
				if !send(ev) {
					return // client disconnected
				}
				if isTerminalStatus(ev.Status) {
					finish(ev.Status)
					return
				}
			case <-ticker.C:
				_, ev, ok := buildStateEvent(ctx, s.deps.Engine, s.deps.Q, *wfID)
				if !ok {
					continue
				}
				if !send(ev) {
					return
				}
				if isTerminalStatus(ev.Status) {
					finish(ev.Status)
					return
				}
			case <-ctx.Done():
				return
			}
		}
	}()
}

func isTerminalStatus(status string) bool {
	switch status {
	case "succeeded", "failed", "skipped", "cancelled":
		return true
	}
	return false
}

// stepsSignature is a cheap change-detection key over the step statuses.
func stepsSignature(steps []engine.StepState) string {
	var b strings.Builder
	for _, s := range steps {
		b.WriteString(s.Name)
		b.WriteByte('=')
		b.WriteString(s.Status)
		b.WriteByte(';')
	}
	return b.String()
}
