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
