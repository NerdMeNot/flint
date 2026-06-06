package server

import (
	"context"
	"encoding/json"

	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/core/observe"
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

	c.JSON(consts.StatusOK, utils.H{
		"workflowId": state.WorkflowID,
		"runId":      state.RunID,
		"status":     state.Status,
		"startedAt":  state.StartedAt,
		"finishedAt": state.FinishedAt,
		"dagWaves":   dagWaves,
		"steps":      state.Steps,
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

	orig, err := s.deps.Q.GetOriginalRunParams(ctx, runID)
	if err != nil {
		apiNotFound(ctx, c, "run not found")
		return
	}

	info, err := s.deps.Q.GetProjectRepoInfo(ctx, orig.ProjectID)
	if err != nil {
		apiInternal(ctx, c, "failed to get project info")
		return
	}

	newRunID := observe.RequestID(ctx)
	err = s.deps.Q.InsertRetryRun(ctx, db.InsertRetryRunParams{
		ID: newRunID, ProjectID: orig.ProjectID, OrgID: orig.OrgID,
		WorkflowFile: orig.WorkflowFile, TriggerRef: orig.TriggerRef,
		CommitSha: orig.CommitSha, Environment: orig.Environment,
	})
	if err != nil {
		apiInternal(ctx, c, "failed to create retry run")
		return
	}

	ref := ""
	if orig.TriggerRef != nil {
		ref = *orig.TriggerRef
	}
	sha := ""
	if orig.CommitSha != nil {
		sha = *orig.CommitSha
	}

	env := ""
	if orig.Environment != nil {
		env = *orig.Environment
	}

	var workflowID string
	if s.deps.Engine != nil {
		var startErr error
		workflowID, startErr = s.deps.Engine.StartWorkflow(ctx, engine.StartWorkflowInput{
			RunID: newRunID, OrgID: orig.OrgID, ProjectID: orig.ProjectID,
			Repo: info.RepoPath, Ref: ref, CommitSHA: sha,
			TriggerType: "retry", TriggeredBy: "api",
			WorkflowFile: orig.WorkflowFile, PipelinePath: info.PipelinePath,
			Environment: env,
		})
		if startErr != nil {
			errMsg := startErr.Error()
			_ = s.deps.Q.FailRunWithError(ctx, db.FailRunWithErrorParams{
				ID:           newRunID,
				ErrorMessage: &errMsg,
			})
		}
	}

	c.JSON(consts.StatusAccepted, utils.H{
		"id": newRunID, "workflowId": workflowID, "status": "pending",
	})
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
