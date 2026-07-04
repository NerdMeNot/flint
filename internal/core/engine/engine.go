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

	"github.com/NerdMeNot/flint/pkg/pipeline"
)

// Engine is the interface for all workflow operations.
// The server uses Engine (no loop). The worker uses Engine + WorkerLoop.
type Engine interface {
	// StartWorkflowWithWaves starts a workflow from an already-resolved DAG.
	// This is the sole entry point: products (CI, Workflows) parse and resolve
	// their own definitions into waves and hand them to the engine, which stays
	// neutral — it never fetches files or knows about a forge. Idempotent for
	// root workflows, in a single Postgres transaction.
	StartWorkflowWithWaves(ctx context.Context, input StartWorkflowInput, waves [][]pipeline.Step) (workflowID string, err error)

	// StartWorkflowSeeded is StartWorkflowWithWaves with carry-over: steps named in
	// seed are pre-completed as succeeded with their prior results, so only the
	// remaining steps run. Backs re-run-failed and retry-from-step.
	StartWorkflowSeeded(ctx context.Context, input StartWorkflowInput, waves [][]pipeline.Step, seed map[string]StepResult) (workflowID string, err error)

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

	// PauseWorkflow halts new step dispatch for a running workflow. In-flight steps
	// run to completion; no new steps are claimed or queued until ResumeWorkflow.
	// Idempotent: pausing a non-running workflow is a no-op.
	PauseWorkflow(ctx context.Context, workflowID string) error

	// ResumeWorkflow returns a paused workflow to running and advances it so
	// newly-eligible steps are queued. Idempotent: resuming a non-paused workflow
	// is a no-op.
	ResumeWorkflow(ctx context.Context, workflowID string) error

	// ResolveStepManually forces a non-terminal step to a terminal outcome
	// (succeeded|failed|skipped) on operator command, recording who and why. The
	// escape hatch for a step an executor will never complete.
	ResolveStepManually(ctx context.Context, workflowID, stepName, outcome, actor, reason string) error

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

	// Git/trigger fields below are CI-supplied run context. The git fields feed
	// the "git" expression namespace (via normalizeInputs) and the step agent's
	// environment; non-CI products leave them empty. The engine's generic surface
	// reads only the Inputs bag above — not these fields.
	Repo        string
	Ref         string
	CommitSHA   string
	TriggerType string
	TriggeredBy string

	RunnerPool   string
	JobNamespace string
	Environment  string // target environment (empty = no env filtering)

	// WorkspaceFlow declares how files move between this run's container
	// steps, so the executor can skip per-run workspace infrastructure that
	// would go unused:
	//   ""          — workspace sync (legacy flat-steps: per-run gRPC pod)
	//   "artifacts" — explicit artifacts via object storage (the ci dialect:
	//                 jobs are self-contained pods; no workspace pod needed)
	//   "none"      — no cross-step file flow (set automatically for runs
	//                 with at most one container step)
	WorkspaceFlow string

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
	Name        string   `json:"name"`
	Status      string   `json:"status"`
	ExecType    string   `json:"execType"`
	Wave        int      `json:"wave"`
	Attempt     int      `json:"attempt"`
	MaxAttempts int      `json:"maxAttempts"`
	DependsOn   []string `json:"dependsOn,omitempty"`
	ExitCode    *int     `json:"exitCode,omitempty"`
	Error       string   `json:"error,omitempty"`
	// ScheduledAt is when the step was queued onto a runner; the gap to
	// StartedAt is runner-queue / pod cold-start wait (UI timeline).
	ScheduledAt *time.Time `json:"scheduledAt,omitempty"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
}

// LoopConfig configures the worker main loop.
type LoopConfig struct {
	// PollInterval is the fallback polling interval used while LISTEN/NOTIFY
	// is NOT connected (degraded mode). Default: 2s.
	PollInterval time.Duration

	// IdlePollInterval is the polling interval while LISTEN/NOTIFY IS healthy —
	// notifications provide the fast path, so polling is only a safety net.
	// Backing off here cuts idle DB load ~90% and plays well with auto-pausing
	// Postgres. Default: 30s.
	IdlePollInterval time.Duration

	// SweepInterval is how often the sweep runs. Default: 5m. Min: 30s.
	SweepInterval time.Duration

	// ClaimBatchSize is how many steps to claim per poll. Default: 20.
	ClaimBatchSize int

	// DispatchGrace is how long a claimed step may sit 'running' without being
	// dispatched (no Job created) before the sweep re-queues it — recovering steps
	// stranded by a worker crash between claim and dispatch. Default: 2m. Min: 30s.
	DispatchGrace time.Duration

	// SigningKey signs the task tokens minted for each step. It MUST match the
	// key the server uses to verify them, and MUST NOT be a pod-exposed value
	// (the internal token is injected into pods, so it cannot be used here).
	SigningKey []byte

	// RunRetentionDays bounds how long terminal runs (and their cascaded
	// workflows/steps) are kept. 0 uses the default (90); negative disables
	// retention (keep forever).
	RunRetentionDays int
}

func (c *LoopConfig) pollInterval() time.Duration {
	if c.PollInterval > 0 {
		return c.PollInterval
	}
	return 2 * time.Second
}

func (c *LoopConfig) idlePollInterval() time.Duration {
	if c.IdlePollInterval > 0 {
		return c.IdlePollInterval
	}
	return 30 * time.Second
}

func (c *LoopConfig) runRetentionDays() int {
	if c.RunRetentionDays != 0 {
		return c.RunRetentionDays
	}
	return 90
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

func (c *LoopConfig) dispatchGrace() time.Duration {
	if c.DispatchGrace >= 30*time.Second {
		return c.DispatchGrace
	}
	if c.DispatchGrace > 0 {
		return 30 * time.Second
	}
	return 2 * time.Minute
}
