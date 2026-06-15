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
