package informer

import (
	"context"
	"fmt"

	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/rs/zerolog/log"
)

// HandleJobCompleted is called by the informer when a K8s Job completes.
// Safety net for when the agent didn't call /internal/complete.
func HandleJobCompleted(ctx context.Context, eng engine.Engine, workflowID, stepName, runID string) {
	log.Info().
		Str("step", stepName).
		Str("runID", runID).
		Msg("informer: job completed, delivering signal")

	// Deliver a step-result signal. The engine's advanceWorkflow will consume it.
	err := eng.DeliverSignal(ctx, workflowID, "step-result", map[string]string{
		"stepName": stepName,
		"runID":    runID,
		"success":  "true",
	})
	if err != nil {
		log.Error().Err(err).Str("step", stepName).Msg("informer: failed to deliver completion signal")
	}
}

// HandleJobFailed is called by the informer when a K8s Job fails.
func HandleJobFailed(ctx context.Context, eng engine.Engine, workflowID, stepName, runID, reason string) {
	if reason == "" {
		reason = "job failed (detected by informer)"
	}

	log.Warn().
		Str("step", stepName).
		Str("runID", runID).
		Str("reason", reason).
		Msg("informer: job failed, delivering signal")

	err := eng.DeliverSignal(ctx, workflowID, "step-result", map[string]string{
		"stepName": stepName,
		"runID":    runID,
		"success":  "false",
		"reason":   reason,
	})
	if err != nil {
		log.Error().Err(err).Str("step", stepName).Msg("informer: failed to deliver failure signal")
	}
}

// HandleJobDeleted is called when a K8s Job is deleted externally.
func HandleJobDeleted(ctx context.Context, eng engine.Engine, workflowID, stepName, runID string) {
	log.Error().
		Str("step", stepName).
		Str("runID", runID).
		Msg("informer: job deleted externally")

	err := eng.DeliverSignal(ctx, workflowID, "step-result", map[string]string{
		"stepName": stepName,
		"runID":    runID,
		"success":  "false",
		"reason":   "job deleted externally",
	})
	if err != nil {
		log.Error().Err(err).Msg("informer: failed to deliver deletion signal")
	}
}

// LookupWorkflowID resolves the workflow ID for a given run ID.
func LookupWorkflowID(ctx context.Context, eng engine.Engine, runID string) (string, error) {
	ws, err := eng.QueryWorkflow(ctx, runID)
	if err != nil {
		return "", fmt.Errorf("informer: lookup workflow for run %s: %w", runID, err)
	}
	return ws.WorkflowID, nil
}
