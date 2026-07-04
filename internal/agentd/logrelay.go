package agentd

import (
	"bytes"
	"context"
	"io"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/types/known/timestamppb"

	agentv1 "github.com/NerdMeNot/flint/protogen/agent/v1"
)

const (
	logBatchLines    = 50
	logBatchInterval = 100 * time.Millisecond
)

// logRelay captures the step's stdio and ships it as LogBatch frames over an
// ExecuteStep stream, listening for server cancellation on the same stream.
// Stream failure degrades to dropped live logs — never to a failed step.
type logRelay struct {
	ctx      context.Context
	stream   agentv1.AgentService_ExecuteStepClient
	onCancel func(reason string)

	mu      sync.Mutex
	pending []*agentv1.LogLine
	seq     int64
	closed  bool

	runID     string
	stepName  string
	matrixKey string

	flushDone chan struct{}
}

func newLogRelay(ctx context.Context, c *client, machineID string, a *agentv1.Assignment, onCancel func(reason string)) *logRelay {
	r := &logRelay{
		ctx:       ctx,
		onCancel:  onCancel,
		runID:     a.GetRunId(),
		stepName:  a.GetStepName(),
		matrixKey: a.GetPayload().GetMatrixKey(),
		flushDone: make(chan struct{}),
	}

	stream, err := c.svc.ExecuteStep(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("agentd: log stream unavailable — step runs without live logs")
		close(r.flushDone)
		return r
	}
	if err := stream.Send(&agentv1.ExecuteStepRequest{Payload: &agentv1.ExecuteStepRequest_Hello{
		Hello: &agentv1.StreamHello{
			MachineId:    machineID,
			AssignmentId: a.GetAssignmentId(),
			TaskToken:    a.GetPayload().GetTaskToken(),
		},
	}}); err != nil {
		log.Warn().Err(err).Msg("agentd: log stream hello failed")
		close(r.flushDone)
		return r
	}
	r.stream = stream

	// Receive loop: cancellations and acks.
	go func() {
		for {
			msg, err := stream.Recv()
			if err != nil {
				return
			}
			if c := msg.GetCancel(); c != nil && r.onCancel != nil {
				r.onCancel(c.GetReason())
			}
		}
	}()
	// Ship loop.
	go r.shipLoop()
	return r
}

// writer returns an io.Writer splitting the named stream into log lines.
func (r *logRelay) writer(stream string) io.Writer {
	return &lineWriter{relay: r, stream: stream}
}

// started announces step start on the stream (best-effort).
func (r *logRelay) started() {
	if r.stream == nil {
		return
	}
	_ = r.stream.Send(&agentv1.ExecuteStepRequest{Payload: &agentv1.ExecuteStepRequest_StepStarted{
		StepStarted: &agentv1.StepStarted{
			RunId: r.runID, StepName: r.stepName, StartedAt: timestamppb.Now(),
		},
	}})
}

func (r *logRelay) shipLoop() {
	defer close(r.flushDone)
	t := time.NewTicker(logBatchInterval)
	defer t.Stop()
	for {
		select {
		case <-r.ctx.Done():
			r.ship()
			return
		case <-t.C:
			if r.isClosed() {
				r.ship()
				return
			}
			r.ship()
		}
	}
}

func (r *logRelay) ship() {
	r.mu.Lock()
	lines := r.pending
	r.pending = nil
	r.mu.Unlock()
	if len(lines) == 0 || r.stream == nil {
		return
	}
	_ = r.stream.Send(&agentv1.ExecuteStepRequest{Payload: &agentv1.ExecuteStepRequest_LogBatch{
		LogBatch: &agentv1.LogBatch{
			RunId: r.runID, StepName: r.stepName, MatrixKey: r.matrixKey, Lines: lines,
		},
	}})
}

func (r *logRelay) push(stream, content string) {
	r.mu.Lock()
	r.seq++
	r.pending = append(r.pending, &agentv1.LogLine{
		Timestamp: timestamppb.Now(), Stream: stream, Content: content, Sequence: r.seq,
	})
	shipNow := len(r.pending) >= logBatchLines
	r.mu.Unlock()
	if shipNow {
		r.ship()
	}
}

// flush ships any pending lines synchronously (called after the step exits).
func (r *logRelay) flush() { r.ship() }

func (r *logRelay) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

func (r *logRelay) close() {
	r.mu.Lock()
	r.closed = true
	r.mu.Unlock()
	if r.stream != nil {
		r.ship()
		_ = r.stream.CloseSend()
	}
}

// lineWriter splits writes into lines for the relay, buffering partials.
type lineWriter struct {
	relay  *logRelay
	stream string
	mu     sync.Mutex
	buf    bytes.Buffer
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	for {
		line, err := w.buf.ReadString('\n')
		if err != nil {
			// Partial line: keep buffering.
			w.buf.WriteString(line)
			break
		}
		w.relay.push(w.stream, trimNewline(line))
	}
	return len(p), nil
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
