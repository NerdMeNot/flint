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
	RunID     string
	OrgID     string
	ProjectID string

	// Kind discriminates the run type. Empty is treated as "ci". It drives
	// product-specific behaviour (e.g. whether a git namespace is synthesized
	// into Inputs). Non-CI kinds populate Inputs themselves.
	Kind string

	// Inputs is the generic, product-neutral context bag the engine reads from
	// during expression evaluation. The engine itself never reaches for typed
	// git fields — it reads namespaces out of this map (git, run, …). CI runs
	// have the git/run namespaces synthesized from the typed fields below by
	// normalizeInputs; other products populate it directly.
	Inputs map[string]any

	// Git/trigger fields below are CI-specific inputs. Repo/CommitSHA/WorkflowFile/
	// PipelinePath drive pipeline + template fetching via the FileGetter, and the
	// git fields are surfaced to the step agent's environment. The engine's
	// generic surface (expression context, run identity) reads only the Inputs
	// bag above — not these fields.
	Repo         string
	Ref          string
	CommitSHA    string
	TriggerType  string
	TriggeredBy  string
	WorkflowFile string
	PipelinePath string

	RunnerPool             string
	JobNamespace           string
	Environment            string // target environment (empty = no env filtering)
	Env                    map[string]string
	RunURL                 string
	PipelineImage          string // default container image from pipeline YAML
	PipelineServiceAccount string // default K8s ServiceAccount from pipeline YAML

	// For child workflows (invoke steps).
	ParentWorkflowID string
	ParentStepName   string
}

// GitContext is the "git" namespace exposed to expressions (git.sha, git.branch,
// git.repoUrl). It is a CI concept; non-CI runs omit it.
type GitContext struct {
	SHA     string
	Branch  string
	RepoURL string
}

// namespace renders the git context as the bag/expression shape. The expr-facing
// key names live here, the single source of truth.
func (g GitContext) namespace() map[string]any {
	return map[string]any{"sha": g.SHA, "branch": g.Branch, "repoUrl": g.RepoURL}
}

// RunContext is the "run" namespace exposed to expressions (run.id, run.trigger).
type RunContext struct {
	ID      string
	Trigger string
}

// namespace renders the run context as the bag/expression shape.
func (r RunContext) namespace() map[string]any {
	return map[string]any{"id": r.ID, "trigger": r.Trigger}
}

// isCI reports whether this run uses CI semantics (the default).
func (in *StartWorkflowInput) isCI() bool {
	return in.Kind == "" || in.Kind == "ci"
}

// normalizeInputs ensures Inputs carries the generic namespaces the engine's
// expression context reads from. For CI runs it synthesizes the git/run
// namespaces from the typed fields so expression evaluation (git.sha,
// run.trigger, …) is unchanged. Callers that already populate a namespace win;
// this only fills gaps. Called before the input is persisted.
func (in *StartWorkflowInput) normalizeInputs() {
	if in.Inputs == nil {
		in.Inputs = map[string]any{}
	}
	if _, ok := in.Inputs["run"]; !ok {
		in.Inputs["run"] = RunContext{ID: in.RunID, Trigger: in.TriggerType}.namespace()
	}
	if in.isCI() {
		if _, ok := in.Inputs["git"]; !ok {
			in.Inputs["git"] = GitContext{SHA: in.CommitSHA, Branch: in.Ref, RepoURL: in.Repo}.namespace()
		}
	}
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
	Name        string     `json:"name"`
	Status      string     `json:"status"`
	ExecType    string     `json:"execType"`
	Wave        int        `json:"wave"`
	Attempt     int        `json:"attempt"`
	MaxAttempts int        `json:"maxAttempts"`
	DependsOn   []string   `json:"dependsOn,omitempty"`
	ExitCode    *int       `json:"exitCode,omitempty"`
	Error       string     `json:"error,omitempty"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
}

// LoopConfig configures the worker main loop.
type LoopConfig struct {
	// PollInterval is the fallback polling interval. Default: 2s.
	PollInterval time.Duration

	// SweepInterval is how often the sweep runs. Default: 5m. Min: 30s.
	SweepInterval time.Duration

	// ClaimBatchSize is how many steps to claim per poll. Default: 20.
	ClaimBatchSize int

	// SigningKey signs the task tokens minted for each step. It MUST match the
	// key the server uses to verify them, and MUST NOT be a pod-exposed value
	// (the internal token is injected into pods, so it cannot be used here).
	SigningKey []byte
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
