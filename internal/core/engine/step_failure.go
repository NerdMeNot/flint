package engine

import (
	"context"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// staleStep is the minimum an engine-initiated failure needs: which attempt to
// fail, and the retry policy that decides whether another one follows.
type staleStep struct {
	id               string
	workflowID       string
	name             string
	attempt          int
	maxAttempts      int
	retryBackoff     string
	retryIntervalSec int
}

// failStepWithRetry marks a step failed through the transition chokepoint and
// schedules the next attempt when its policy allows one.
//
// It exists so that engine-initiated failures — the timeout timer and the
// deadline sweep — express the same contract CompleteStep and failStepAndAdvance
// already honour: a failure is a failure, and `retry:` applies to all of them.
// Both of those paths previously updated the step row directly, which meant the
// policy was never consulted and `retry: { attempts: 3 }` quietly meant "three
// attempts, unless it times out" — the one failure mode people most often add
// retries for.
//
// Runs inside the caller's transaction, so the failure, its history event, the
// parked next attempt and its backoff timer all commit together or not at all.
func failStepWithRetry(ctx context.Context, qtx *db.Queries, s staleStep, from, reason, eventType string) error {
	result := StepResult{StepName: s.name, Success: false, Error: reason}
	if err := transitionStep(ctx, qtx, stepTransition{
		stepID:     s.id,
		workflowID: s.workflowID,
		stepName:   s.name,
		attempt:    s.attempt,
		from:       from,
		to:         stepFailed,
		eventType:  eventType,
		result:     &result,
		actor:      actorEngine,
		reason:     reason,
	}); err != nil {
		return err
	}
	_, err := maybeScheduleRetry(ctx, qtx, s.id, s.workflowID, s.name,
		s.attempt, s.maxAttempts, s.retryBackoff, s.retryIntervalSec)
	return err
}
