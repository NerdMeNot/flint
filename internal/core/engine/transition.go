package engine

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// transition.go is the single chokepoint for state changes. Every step/workflow
// status transition flows through transitionStep / transitionWorkflow, which
// (1) validate the move against the allowed-transitions tables in status.go,
// (2) update the current-state row with the right timestamp semantics, and
// (3) append an immutable engine_events row in the SAME transaction.
//
// This is CQRS-lite: the steps/workflows rows remain the authoritative current
// state (claim predicates, advancement read them); engine_events is the durable
// history sidecar that powers the run timeline, retry history, and audit. We
// never rebuild execution state by replaying events — that is the Temporal
// complexity this engine deliberately avoids.
//
// Bulk recovery paths (claim, sweep, cancel) that mutate many rows in one SQL
// statement update the rows in SQL and then call emitStepEvent for each affected
// step, rather than routing through transitionStep — the SQL already performed
// the (inherently valid) status change.

// stepTransition describes a single-step status change.
type stepTransition struct {
	stepID     string
	workflowID string
	stepName   string
	attempt    int
	from       string
	to         string
	// eventType overrides the history event_type; defaults to `to` when empty
	// (e.g. "timed_out" for a timeout-driven failure vs a plain "failed").
	eventType string
	// result, when non-nil, is persisted to steps.result and its exitCode/error
	// are folded into the event metadata. Required for terminal transitions that
	// carry a step outcome.
	result   *StepResult
	actor    string
	reason   string
	metadata map[string]any
	// force bypasses the allowed-transitions table (operator intervention only).
	force bool
}

// transitionStep validates, applies, and records a single step status change.
func transitionStep(ctx context.Context, qtx *db.Queries, t stepTransition) error {
	if !t.force && !stepTransitionAllowed(t.from, t.to) {
		return fmt.Errorf("engine: illegal step transition %s→%s for %s/%s (attempt %d)",
			t.from, t.to, t.workflowID, t.stepName, t.attempt)
	}

	// 1. Update the current-state row, preserving each status's timestamp rules.
	switch t.to {
	case stepQueued:
		if err := qtx.SetStepQueued(ctx, t.stepID); err != nil {
			return fmt.Errorf("engine: set step queued: %w", err)
		}
	case stepSkipped:
		if err := qtx.SetStepSkipped(ctx, t.stepID); err != nil {
			return fmt.Errorf("engine: set step skipped: %w", err)
		}
	case stepSucceeded, stepFailed, stepCancelled:
		var resultJSON []byte
		if t.result != nil {
			resultJSON = mustJSON(*t.result)
		}
		if err := qtx.UpdateStepResult(ctx, db.UpdateStepResultParams{
			ID:     t.stepID,
			Status: t.to,
			Result: resultJSON,
		}); err != nil {
			return fmt.Errorf("engine: update step result: %w", err)
		}
	default:
		// pending (pause re-queue), running/waiting set out-of-band by claim.
		if err := qtx.SetStepStatus(ctx, db.SetStepStatusParams{ID: t.stepID, Status: t.to}); err != nil {
			return fmt.Errorf("engine: set step status: %w", err)
		}
	}

	// 2. Append the history event.
	emitStepEvent(ctx, qtx, t)
	return nil
}

// emitStepEvent appends a step-scoped engine_events row. Used by transitionStep
// and by bulk paths that already performed the status update in SQL. A failure to
// record history is logged but never fails the surrounding transaction — losing an
// audit row must not wedge workflow advancement.
func emitStepEvent(ctx context.Context, qtx *db.Queries, t stepTransition) {
	eventType := t.eventType
	if eventType == "" {
		eventType = t.to
	}
	actor := t.actor
	if actor == "" {
		actor = actorEngine
	}

	meta := t.metadata
	if t.result != nil {
		if meta == nil {
			meta = map[string]any{}
		}
		if t.result.Error != "" {
			meta["error"] = t.result.Error
		}
		meta["exitCode"] = t.result.ExitCode
	}

	stepName := t.stepName
	if err := qtx.InsertEngineEvent(ctx, db.InsertEngineEventParams{
		WorkflowID: t.workflowID,
		StepName:   &stepName,
		Attempt:    pgInt4(t.attempt),
		EventType:  eventType,
		FromStatus: nullableStatus(t.from),
		ToStatus:   nullableStatus(t.to),
		Actor:      actor,
		Reason:     nullableStr(t.reason),
		Metadata:   metaJSON(meta),
	}); err != nil {
		log.Warn().Err(err).Str("workflow", t.workflowID).Str("step", t.stepName).
			Str("event", eventType).Msg("engine: failed to record step event")
	}
}

// workflowTransition describes a workflow status change.
type workflowTransition struct {
	workflowID string
	from       string
	to         string
	eventType  string
	actor      string
	reason     string
	metadata   map[string]any
}

// emitWorkflowEvent appends a workflow-scoped (step_name NULL) history event. The
// status update itself is performed by the caller's dedicated query (FinishWorkflow,
// CancelWorkflow, PauseWorkflow, …) so this only records history.
func emitWorkflowEvent(ctx context.Context, qtx *db.Queries, w workflowTransition) {
	eventType := w.eventType
	if eventType == "" {
		eventType = w.to
	}
	actor := w.actor
	if actor == "" {
		actor = actorEngine
	}
	if err := qtx.InsertEngineEvent(ctx, db.InsertEngineEventParams{
		WorkflowID: w.workflowID,
		StepName:   nil,
		Attempt:    pgtype.Int4{},
		EventType:  eventType,
		FromStatus: nullableStatus(w.from),
		ToStatus:   nullableStatus(w.to),
		Actor:      actor,
		Reason:     nullableStr(w.reason),
		Metadata:   metaJSON(w.metadata),
	}); err != nil {
		log.Warn().Err(err).Str("workflow", w.workflowID).Str("event", eventType).
			Msg("engine: failed to record workflow event")
	}
}

// ── small encoders ───────────────────────────────────────────────────────────

func pgInt4(v int) pgtype.Int4 {
	if v < 0 {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(v), Valid: true}
}

func nullableStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nullableStatus(s string) *string { return nullableStr(s) }

func metaJSON(m map[string]any) []byte {
	if len(m) == 0 {
		return nil
	}
	return mustJSON(m)
}
