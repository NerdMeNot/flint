package server

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// runEventDTO is the API shape of one engine_events row for the run timeline.
type runEventDTO struct {
	StepName *string         `json:"stepName,omitempty"`
	Attempt  *int            `json:"attempt,omitempty"`
	Event    string          `json:"event"`
	From     *string         `json:"from,omitempty"`
	To       *string         `json:"to,omitempty"`
	Actor    string          `json:"actor"`
	Reason   *string         `json:"reason,omitempty"`
	Metadata json.RawMessage `json:"metadata,omitempty"`
	At       interface{}     `json:"at"`
}

// handleGetRunEvents returns the durable transition timeline for a run, oldest
// first — the per-attempt history that the run-detail timeline view renders.
func (s *Server) handleGetRunEvents(ctx context.Context, c *app.RequestContext) {
	runID := c.Param("id")
	workflowID, err := s.deps.Q.GetRunWorkflowID(ctx, runID)
	if err != nil || workflowID == nil {
		apiNotFound(ctx, c, "run not found or no workflow")
		return
	}
	rows, err := s.deps.Q.ListEngineEventsByWorkflow(ctx, *workflowID)
	if err != nil {
		apiInternal(ctx, c, "failed to load run events")
		return
	}
	events := make([]runEventDTO, 0, len(rows))
	for _, r := range rows {
		e := runEventDTO{
			StepName: r.StepName,
			Event:    r.EventType,
			From:     r.FromStatus,
			To:       r.ToStatus,
			Actor:    r.Actor,
			Reason:   r.Reason,
			At:       r.CreatedAt,
		}
		if r.Attempt.Valid {
			a := int(r.Attempt.Int32)
			e.Attempt = &a
		}
		if len(r.Metadata) > 0 {
			e.Metadata = json.RawMessage(r.Metadata)
		}
		events = append(events, e)
	}
	c.JSON(consts.StatusOK, utils.H{"runId": runID, "events": events})
}

func (s *Server) handleGetRunSteps(ctx context.Context, c *app.RequestContext) {
	runID := c.Param("id")

	workflowID, err := s.deps.Q.GetRunWorkflowID(ctx, runID)
	if err != nil || workflowID == nil {
		apiNotFound(ctx, c, "run not found or no workflow")
		return
	}

	state, err := s.deps.Engine.QueryWorkflow(ctx, *workflowID)
	if err != nil {
		apiInternal(ctx, c, "failed to query workflow")
		return
	}

	dagWaves, _ := s.deps.Q.GetWorkflowDAGWaves(ctx, *workflowID)
	if len(dagWaves) == 0 {
		dagWaves = []byte("[]")
	}

	c.JSON(consts.StatusOK, utils.H{
		"workflowId": state.WorkflowID,
		"runId":      state.RunID,
		"status":     state.Status,
		"startedAt":  state.StartedAt,
		"finishedAt": state.FinishedAt,
		// Raw JSON so the array isn't base64-encoded (it's a jsonb []byte column).
		"dagWaves": json.RawMessage(dagWaves),
		"steps":    state.Steps,
	})
}

func (s *Server) handleCancelRun(ctx context.Context, c *app.RequestContext) {
	runID := c.Param("id")

	workflowID, err := s.deps.Q.GetRunWorkflowID(ctx, runID)
	if err != nil || workflowID == nil {
		apiNotFound(ctx, c, "run not found")
		return
	}

	if err := s.deps.Engine.CancelWorkflow(ctx, *workflowID); err != nil {
		apiInternal(ctx, c, "failed to cancel workflow")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true})
}

func (s *Server) handlePauseRun(ctx context.Context, c *app.RequestContext) {
	runID := c.Param("id")
	workflowID, err := s.deps.Q.GetRunWorkflowID(ctx, runID)
	if err != nil || workflowID == nil {
		apiNotFound(ctx, c, "run not found")
		return
	}
	if err := s.deps.Engine.PauseWorkflow(ctx, *workflowID); err != nil {
		apiInternal(ctx, c, "failed to pause workflow")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true, "status": "paused"})
}

func (s *Server) handleResumeRun(ctx context.Context, c *app.RequestContext) {
	runID := c.Param("id")
	workflowID, err := s.deps.Q.GetRunWorkflowID(ctx, runID)
	if err != nil || workflowID == nil {
		apiNotFound(ctx, c, "run not found")
		return
	}
	if err := s.deps.Engine.ResumeWorkflow(ctx, *workflowID); err != nil {
		apiInternal(ctx, c, "failed to resume workflow")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true, "status": "running"})
}

func (s *Server) handleRetryRun(ctx context.Context, c *app.RequestContext) {
	runID := c.Param("id")
	if s.deps.Runs == nil {
		apiInternal(ctx, c, "run creation unavailable")
		return
	}
	newRunID, workflowID, err := s.deps.Runs.Rerun(ctx, runID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			apiNotFound(ctx, c, err.Error())
		} else {
			apiBadRequest(ctx, c, err.Error())
		}
		return
	}
	c.JSON(consts.StatusAccepted, utils.H{"id": newRunID, "workflowId": workflowID, "status": "pending"})
}

func (s *Server) handleResolveStep(ctx context.Context, c *app.RequestContext) {
	runID := c.Param("id")
	stepName := c.Param("step")
	var req struct {
		Outcome string `json:"outcome"`
		Reason  string `json:"reason"`
	}
	if c.BindJSON(&req) != nil || req.Outcome == "" {
		apiBadRequest(ctx, c, "outcome is required (succeeded|failed|skipped)")
		return
	}
	workflowID, err := s.deps.Q.GetRunWorkflowID(ctx, runID)
	if err != nil || workflowID == nil {
		apiNotFound(ctx, c, "run not found")
		return
	}
	actor := ""
	if claims := claimsFromCtx(ctx); claims != nil {
		actor = claims.Email
	}
	if err := s.deps.Engine.ResolveStepManually(ctx, *workflowID, stepName, req.Outcome, actor, req.Reason); err != nil {
		if strings.Contains(err.Error(), "not found") {
			apiNotFound(ctx, c, err.Error())
		} else {
			apiBadRequest(ctx, c, err.Error())
		}
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true, "status": req.Outcome})
}

func (s *Server) handleRerunFailed(ctx context.Context, c *app.RequestContext) {
	runID := c.Param("id")
	if s.deps.Runs == nil {
		apiInternal(ctx, c, "run creation unavailable")
		return
	}
	newRunID, workflowID, err := s.deps.Runs.RerunFailed(ctx, runID)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			apiNotFound(ctx, c, err.Error())
		} else {
			apiBadRequest(ctx, c, err.Error())
		}
		return
	}
	c.JSON(consts.StatusAccepted, utils.H{"id": newRunID, "workflowId": workflowID, "status": "pending"})
}

func (s *Server) handleRetryFromStep(ctx context.Context, c *app.RequestContext) {
	runID := c.Param("id")
	stepName := c.Param("step")
	if s.deps.Runs == nil {
		apiInternal(ctx, c, "run creation unavailable")
		return
	}
	newRunID, workflowID, err := s.deps.Runs.RetryFromStep(ctx, runID, stepName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			apiNotFound(ctx, c, err.Error())
		} else {
			apiBadRequest(ctx, c, err.Error())
		}
		return
	}
	c.JSON(consts.StatusAccepted, utils.H{"id": newRunID, "workflowId": workflowID, "status": "pending"})
}

func (s *Server) handleApproveGate(ctx context.Context, c *app.RequestContext) {
	runID := c.Param("id")
	var req struct {
		StepName string `json:"stepName"`
		Comment  string `json:"comment"`
	}
	if c.BindJSON(&req) != nil || req.StepName == "" {
		apiBadRequest(ctx, c, "stepName is required")
		return
	}

	workflowID, err := s.deps.Q.GetRunWorkflowID(ctx, runID)
	if err != nil || workflowID == nil {
		apiNotFound(ctx, c, "run not found")
		return
	}

	claims := claimsFromCtx(ctx)
	approvedBy := "unknown"
	if claims != nil {
		approvedBy = claims.Email
	}

	// Enforce gate approver restrictions.
	step, err := s.deps.Q.GetStepByWorkflowAndName(ctx, db.GetStepByWorkflowAndNameParams{
		WorkflowID: *workflowID,
		Name:       req.StepName,
	})
	if err == nil && step.ExecType == "gate" && step.StepDef != nil {
		var gateDef struct {
			Gate struct {
				Approvers []string `json:"approvers"`
			} `json:"gate"`
		}
		if json.Unmarshal(step.StepDef, &gateDef) == nil && len(gateDef.Gate.Approvers) > 0 {
			// Get the user's ID for team membership checks.
			var userID string
			if claims != nil {
				org, _ := s.deps.Q.GetOrg(ctx)
				user, userErr := s.deps.Q.GetUserByEmail(ctx, db.GetUserByEmailParams{
					OrgID: org.ID,
					Email: claims.Email,
				})
				if userErr == nil {
					userID = user.ID
				}
			}

			authorized, authErr := auth.IsAuthorizedApprover(ctx, s.deps.Q,
				claims.OrgID, userID, claims.Email, gateDef.Gate.Approvers)
			if authErr != nil {
				apiInternal(ctx, c, "failed to check approver authorization")
				return
			}
			if !authorized {
				apiForbidden(ctx, c, "you are not an authorized approver for this gate")
				return
			}
		}
	}

	err = s.deps.Engine.DeliverSignal(ctx, *workflowID, "gate-"+req.StepName, map[string]string{
		"action": "approve", "approvedBy": approvedBy, "comment": req.Comment,
	})
	if err != nil {
		apiInternal(ctx, c, "failed to approve gate")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true})
}

func (s *Server) handleRejectGate(ctx context.Context, c *app.RequestContext) {
	runID := c.Param("id")
	var req struct {
		StepName string `json:"stepName"`
		Reason   string `json:"reason"`
	}
	if c.BindJSON(&req) != nil || req.StepName == "" {
		apiBadRequest(ctx, c, "stepName is required")
		return
	}

	workflowID, err := s.deps.Q.GetRunWorkflowID(ctx, runID)
	if err != nil || workflowID == nil {
		apiNotFound(ctx, c, "run not found")
		return
	}

	err = s.deps.Engine.DeliverSignal(ctx, *workflowID, "gate-reject-"+req.StepName, map[string]string{
		"action": "reject", "reason": req.Reason,
	})
	if err != nil {
		apiInternal(ctx, c, "failed to reject gate")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true})
}

// handleSendSignal delivers an external signal to a run's workflow, resolving any
// `wait` step parked on that signal name. The payload (a flat JSON object) is
// captured into the wait step's outputs so downstream steps can read
// steps.<name>.<key>. Reserved signal names (the engine's gate/step-result
// channels) are rejected so this generic endpoint can't be used to spoof a gate
// approval or a step completion.
func (s *Server) handleSendSignal(ctx context.Context, c *app.RequestContext) {
	runID := c.Param("id")
	var req struct {
		Name    string         `json:"name"`
		Payload map[string]any `json:"payload"`
	}
	if c.BindJSON(&req) != nil || req.Name == "" {
		apiBadRequest(ctx, c, "name is required")
		return
	}
	if isReservedSignalName(req.Name) {
		apiBadRequest(ctx, c, "signal name is reserved")
		return
	}

	workflowID, err := s.deps.Q.GetRunWorkflowID(ctx, runID)
	if err != nil || workflowID == nil {
		apiNotFound(ctx, c, "run not found")
		return
	}

	payload := req.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	if err := s.deps.Engine.DeliverSignal(ctx, *workflowID, req.Name, payload); err != nil {
		apiInternal(ctx, c, "failed to deliver signal")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true})
}

// isReservedSignalName reports whether a signal name belongs to one of the
// engine's internal channels (gate approve/reject, step-result) and so must not be
// settable through the public signal endpoint.
func isReservedSignalName(name string) bool {
	return name == "step-result" || strings.HasPrefix(name, "gate-")
}
