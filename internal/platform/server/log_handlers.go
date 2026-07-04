package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/pkg/logsink"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// handleGetRunLogs returns the combined per-step logs for a run as
// { logs: { stepName: text } } — the UI's "All output" view.
// GET /api/v1/runs/:id/logs
func (s *Server) handleGetRunLogs(ctx context.Context, c *app.RequestContext) {
	runID := c.Param("id")
	if s.deps.Logs == nil {
		apiInternal(ctx, c, "log sink not configured")
		return
	}
	orgID, err := s.deps.Q.GetRunOrgID(ctx, runID)
	if err != nil {
		apiNotFound(ctx, c, "run not found")
		return
	}
	wfID, err := s.deps.Q.GetRunWorkflowID(ctx, runID)
	if err != nil || wfID == nil {
		c.JSON(consts.StatusOK, utils.H{"logs": utils.H{}})
		return
	}
	state, err := s.deps.Engine.QueryWorkflow(ctx, *wfID)
	if err != nil {
		apiInternal(ctx, c, "failed to query workflow")
		return
	}

	logs := map[string]string{}
	for _, st := range state.Steps {
		if st.StartedAt == nil {
			continue // not started → no logs
		}
		lines, rerr := s.deps.Logs.Read(ctx, logsink.LogRef{OrgID: orgID, RunID: runID, StepName: st.Name})
		if rerr != nil {
			continue
		}
		var sb strings.Builder
		for _, l := range lines {
			sb.WriteString(l.Content)
			sb.WriteByte('\n')
		}
		logs[st.Name] = sb.String()
	}
	c.JSON(consts.StatusOK, utils.H{"logs": logs})
}

// handleGetStepLogs returns historical logs for a step.
// GET /api/v1/runs/:id/logs/:step
func (s *Server) handleGetStepLogs(ctx context.Context, c *app.RequestContext) {
	runID := c.Param("id")
	stepName := c.Param("step")

	if s.deps.Logs == nil {
		apiInternal(ctx, c, "log sink not configured")
		return
	}

	orgID, err := s.deps.Q.GetRunOrgID(ctx, runID)
	if err != nil {
		apiNotFound(ctx, c, "run not found")
		return
	}

	ref := logsink.LogRef{
		OrgID:     orgID,
		RunID:     runID,
		StepName:  stepName,
		MatrixKey: queryString(c, "matrix_key"),
	}

	lines, err := s.deps.Logs.Read(ctx, ref)
	if err != nil {
		apiInternal(ctx, c, "failed to read logs")
		return
	}

	if lines == nil {
		lines = []logsink.LogLine{}
	}

	// Check if step is still running.
	stepStatus, _ := s.deps.Q.GetStepStatus(ctx, db.GetStepStatusParams{
		ID:   runID,
		Name: stepName,
	})

	complete := stepStatus == "succeeded" || stepStatus == "failed" ||
		stepStatus == "skipped" || stepStatus == "cancelled"

	c.JSON(consts.StatusOK, utils.H{
		"lines":    lines,
		"hasMore":  false,
		"complete": complete,
	})
}

// handleStreamStepLogs streams log lines via Server-Sent Events (SSE).
// GET /api/v1/runs/:id/logs/:step/stream
//
// Events:
//
//	event: log   data: {"lines":[...]}
//	event: done  data: {"status":"succeeded"}
//
// Uses Hertz's SetBodyStream with an io.Pipe for true streaming — bytes are
// flushed to the wire as they are written, not buffered until handler return.
// The stream closes when the step reaches a terminal state, or when the
// client disconnects.
func (s *Server) handleStreamStepLogs(ctx context.Context, c *app.RequestContext) {
	runID := c.Param("id")
	stepName := c.Param("step")

	if s.deps.LogBroadcast == nil {
		apiError(ctx, c, consts.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "log streaming not enabled")
		return
	}

	// SSE headers — must be set before SetBodyStream.
	c.Response.Header.SetContentType("text/event-stream")
	c.Response.Header.Set("Cache-Control", "no-cache")
	c.Response.Header.Set("Connection", "keep-alive")
	c.Response.Header.Set("X-Accel-Buffering", "no") // disable nginx buffering

	pr, pw := io.Pipe()
	c.Response.SetBodyStream(pr, -1) // -1 = chunked transfer encoding

	sub := s.deps.LogBroadcast.Subscribe(runID, stepName)

	// The streaming goroutine writes SSE events to the pipe. When it's done
	// (step complete or client gone), it closes the pipe writer, which signals
	// Hertz to end the response.
	go func() {
		defer pw.Close()
		defer s.deps.LogBroadcast.Unsubscribe(runID, stepName, sub)

		// Send existing (historical) lines first so the client doesn't miss anything.
		if s.deps.Logs != nil {
			orgID, _ := s.deps.Q.GetRunOrgID(ctx, runID)
			ref := logsink.LogRef{
				OrgID:     orgID,
				RunID:     runID,
				StepName:  stepName,
				MatrixKey: queryString(c, "matrix_key"),
			}
			if hist, err := s.deps.Logs.Read(ctx, ref); err == nil && len(hist) > 0 {
				if writeSSEEvent(pw, "log", hist) != nil {
					return
				}
			}
		}

		completionTicker := time.NewTicker(2 * time.Second)
		defer completionTicker.Stop()

		for {
			select {
			case lines, ok := <-sub:
				if !ok {
					return
				}
				if writeSSEEvent(pw, "log", lines) != nil {
					return // client disconnected
				}

			case <-completionTicker.C:
				stepStatus, _ := s.deps.Q.GetStepStatus(ctx, db.GetStepStatusParams{
					ID:   runID,
					Name: stepName,
				})
				if stepStatus == "succeeded" || stepStatus == "failed" ||
					stepStatus == "skipped" || stepStatus == "cancelled" {
					_ = writeSSEEvent(pw, "done", map[string]string{"status": stepStatus})
					return
				}

			case <-ctx.Done():
				return
			}
		}
	}()
}

func writeSSEEvent(w io.Writer, event string, data any) error {
	payload, _ := json.Marshal(data)
	_, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
	return err
}
