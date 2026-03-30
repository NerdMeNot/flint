// Package engine provides the embedded workflow engine for Flint.
// It replaces Temporal with a Postgres-backed DAG executor.
//
// All state lives in Postgres. No external services. No replay.
// Workflows advance through DAG waves as steps complete.
//
// Durability guarantees:
//   - Workflow survives worker restart (all state in Postgres)
//   - Step survives agent crash (informer + sweep detect it)
//   - No double execution (UNIQUE constraints + idempotent CompleteStep)
//   - Atomic state transitions (single Postgres transaction)
//   - Timers are durable (explicit rows, not in-memory)
//   - Signals are durable (explicit rows, checked during advancement)
//   - Multi-replica safe (SKIP LOCKED for claiming, FOR UPDATE for advancement)
package engine

import (
	"context"
	"time"
)

// Engine is the interface for all workflow operations.
// The server uses Engine (no loop). The worker uses Engine + WorkerLoop.
type Engine interface {
	// StartWorkflow creates a workflow execution: fetches pipeline YAML,
	// resolves the DAG, creates step rows, and queues wave-0 steps.
	// All in a single Postgres transaction.
	StartWorkflow(ctx context.Context, input StartWorkflowInput) (workflowID string, err error)

	// CompleteStep reports that a step has finished (success or failure).
	// Validates the task token, updates step status, handles retries,
	// and advances the workflow — all in one transaction.
	// Idempotent: calling twice with the same token is safe.
	CompleteStep(ctx context.Context, taskToken string, result StepResult) error

	// DeliverSignal writes a signal for a workflow (gate approval, informer event, etc.).
	// Triggers NOTIFY so the worker loop wakes up.
	DeliverSignal(ctx context.Context, workflowID string, signalName string, payload any) error

	// CancelWorkflow marks a workflow and all non-terminal steps as cancelled.
	CancelWorkflow(ctx context.Context, workflowID string) error

	// QueryWorkflow returns the current state of a workflow and its steps.
	QueryWorkflow(ctx context.Context, workflowID string) (*WorkflowState, error)

	// Close releases resources.
	Close()
}

// WorkerLoop runs the main polling loop that advances workflows.
type WorkerLoop interface {
	Run(ctx context.Context) error
}

// StartWorkflowInput contains everything needed to create a workflow.
type StartWorkflowInput struct {
	RunID        string
	OrgID        string
	ProjectID    string
	Repo         string
	Ref          string
	CommitSHA    string
	TriggerType  string
	TriggeredBy  string
	WorkflowFile string
	PipelinePath string
	RunnerPool   string
	JobNamespace string
	Env          map[string]string
	RunURL       string

	// For child workflows (invoke steps).
	ParentWorkflowID string
	ParentStepName   string
}

// StepResult is the result of a completed step.
type StepResult struct {
	StepName string            `json:"stepName"`
	Success  bool              `json:"success"`
	ExitCode int               `json:"exitCode"`
	Outputs  map[string]string `json:"outputs,omitempty"`
	Error    string            `json:"error,omitempty"`
}

// WorkflowState is the snapshot returned by QueryWorkflow.
type WorkflowState struct {
	WorkflowID string
	RunID      string
	Status     string
	Steps      []StepState
	StartedAt  *time.Time
	FinishedAt *time.Time
}

// StepState is one step within a workflow state query.
type StepState struct {
	Name     string
	Status   string
	Wave     int
	Attempt  int
	ExitCode *int
	Error    string
}

// LoopConfig configures the worker main loop.
type LoopConfig struct {
	// PollInterval is the fallback polling interval. Default: 2s.
	PollInterval time.Duration

	// SweepInterval is how often the sweep runs. Default: 5m. Min: 30s.
	SweepInterval time.Duration

	// ClaimBatchSize is how many steps to claim per poll. Default: 20.
	ClaimBatchSize int
}

func (c *LoopConfig) pollInterval() time.Duration {
	if c.PollInterval > 0 {
		return c.PollInterval
	}
	return 2 * time.Second
}

func (c *LoopConfig) sweepInterval() time.Duration {
	if c.SweepInterval >= 30*time.Second {
		return c.SweepInterval
	}
	if c.SweepInterval > 0 {
		return 30 * time.Second
	}
	return 5 * time.Minute
}

func (c *LoopConfig) claimBatchSize() int {
	if c.ClaimBatchSize > 0 {
		return c.ClaimBatchSize
	}
	return 20
}
