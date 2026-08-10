package engine

// Canonical state strings for the engine. These mirror the CHECK constraints in
// internal/core/dbkit/migrations/001_initial_schema.sql — keep them in sync.
// SQL queries can't reference Go constants, so the literals still appear in
// queries/*.sql; these exist so engine Go code is greppable and adding a new
// state is a deliberate, visible change rather than a scattered string edit.

// Step statuses (steps_status_check).
const (
	stepPending   = "pending"
	stepRetryWait = "retry_wait" // retry attempt parked until its backoff timer fires
	stepQueued    = "queued"
	stepRunning   = "running"
	stepWaiting   = "waiting" // gate/wait step parked until a signal or timeout
	stepSucceeded = "succeeded"
	stepFailed    = "failed"
	stepSkipped   = "skipped"
	stepCancelled = "cancelled"
)

// Step exec types (steps_exec_type_check).
const (
	execRun   = "run"
	execUse   = "use"
	execSteps = "steps"
	execGate  = "gate"
	execWait  = "wait" // pause until an external signal (Workflows primitive)
)

// Timer types (timers_timer_type_check).
const (
	timerTimeout      = "timeout"
	timerGateTimeout  = "gate_timeout"
	timerRetryBackoff = "retry_backoff"
	timerWaitTimeout  = "wait_timeout"
)

// isTerminal reports whether a step status is final (no further transitions).
func isTerminal(status string) bool {
	switch status {
	case stepSucceeded, stepFailed, stepSkipped, stepCancelled:
		return true
	default:
		return false
	}
}

// Workflow statuses (workflows_status_check).
const (
	wfPending   = "pending"
	wfRunning   = "running"
	wfPaused    = "paused"
	wfSucceeded = "succeeded"
	wfFailed    = "failed"
	wfCancelled = "cancelled"
)

// allowedStepTransitions is the explicit step-status transition table. A step
// status may only move to one of its listed targets; anything else is a
// programming error and is rejected by transitionStep (unless force is set, for
// operator intervention). Terminal statuses have no entry — they never transition.
//
// This is the single, greppable source of truth for the step state machine. The
// claim path (queued→running|waiting) is bulk SQL and validated here too.
var allowedStepTransitions = map[string][]string{
	// pending→failed: a step whose if: expression errors at evaluation time
	// fails loudly without ever running (silently skipping it would hide a
	// pipeline-authoring bug).
	stepPending:   {stepQueued, stepSkipped, stepFailed, stepCancelled},
	stepRetryWait: {stepQueued, stepCancelled},
	// queued→failed: the machine holding this step's assignment died before the
	// agent started it. The step never ran, but it is not going to — the fleet's
	// machine-lost signal has to be able to fail it. Without this edge the signal
	// hit an illegal transition, was swallowed, and the step sat queued until the
	// deadline sweep hours later, which is precisely the wait the signal exists
	// to avoid.
	stepQueued:  {stepRunning, stepWaiting, stepPending, stepSkipped, stepFailed, stepCancelled},
	stepRunning: {stepSucceeded, stepFailed, stepCancelled},
	stepWaiting: {stepSucceeded, stepFailed, stepCancelled},
}

// allowedWorkflowTransitions is the workflow-status transition table.
var allowedWorkflowTransitions = map[string][]string{
	wfPending: {wfRunning, wfCancelled},
	wfRunning: {wfPaused, wfSucceeded, wfFailed, wfCancelled},
	wfPaused:  {wfRunning, wfCancelled},
}

// stepTransitionAllowed reports whether from→to is a legal step transition.
// A no-op (from == to) is allowed so idempotent re-applies don't error.
func stepTransitionAllowed(from, to string) bool {
	if from == to {
		return true
	}
	for _, t := range allowedStepTransitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

// workflowTransitionAllowed reports whether from→to is a legal workflow transition.
func workflowTransitionAllowed(from, to string) bool {
	if from == to {
		return true
	}
	for _, t := range allowedWorkflowTransitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

// Engine event actors — who caused a transition (engine_events.actor).
const (
	actorEngine   = "engine"
	actorAgent    = "agent"
	actorInformer = "informer"
	actorSweep    = "sweep"
	// operator actions use "operator:<userID>" — see actorOperator.
)

func actorOperator(userID string) string {
	if userID == "" {
		return "operator"
	}
	return "operator:" + userID
}
