// Package logsink provides the LogSink interface for storing and retrieving
// pipeline step logs. S3Sink is the production implementation.
// FilesystemSink exists for tests only.
package logsink

import (
	"context"
	"time"
)

// LogSink is the interface for log storage and retrieval.
type LogSink interface {
	// Write appends log lines for a step.
	Write(ctx context.Context, ref LogRef, lines []LogLine) error

	// Read retrieves all log lines for a step.
	Read(ctx context.Context, ref LogRef) ([]LogLine, error)

	// Tail returns a channel that receives new log lines in real time.
	// The channel is closed when the context is cancelled.
	Tail(ctx context.Context, ref LogRef) (<-chan LogLine, error)
}

// LogRef identifies a specific step's log stream.
type LogRef struct {
	OrgID     string
	RunID     string
	StepName  string
	MatrixKey string // empty for non-matrix steps
}

// LogLine is a single line of log output.
type LogLine struct {
	Timestamp time.Time `json:"timestamp"`
	Stream    string    `json:"stream"` // "stdout" or "stderr"
	Content   string    `json:"content"`
}

// Path returns a filesystem-safe path for this log ref.
func (r LogRef) Path() string {
	if r.MatrixKey != "" {
		return r.OrgID + "/" + r.RunID + "/" + r.StepName + "/" + r.MatrixKey
	}
	return r.OrgID + "/" + r.RunID + "/" + r.StepName
}
