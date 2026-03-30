package server

import (
	"context"

	"github.com/NerdMeNot/flint/internal/db"
	"github.com/NerdMeNot/flint/pkg/logsink"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// handleGetStepLogs returns historical logs for a step.
// GET /api/v1/runs/:id/logs/:step
func (s *Server) handleGetStepLogs(ctx context.Context, c *app.RequestContext) {
	runID := c.Param("id")
	stepName := c.Param("step")

	if s.deps.Logs == nil {
		apiInternal(ctx, c, "log sink not configured")
		return
	}

	orgID, err := s.deps.Q.GetRunOrgID(ctx, runID)
	if err != nil {
		apiNotFound(ctx, c, "run not found")
		return
	}

	ref := logsink.LogRef{
		OrgID:     orgID,
		RunID:     runID,
		StepName:  stepName,
		MatrixKey: queryString(c, "matrix_key"),
	}

	lines, err := s.deps.Logs.Read(ctx, ref)
	if err != nil {
		apiInternal(ctx, c, "failed to read logs")
		return
	}

	if lines == nil {
		lines = []logsink.LogLine{}
	}

	// Check if step is still running.
	stepStatus, _ := s.deps.Q.GetStepStatus(ctx, db.GetStepStatusParams{
		ID:   runID,
		Name: stepName,
	})

	complete := stepStatus == "succeeded" || stepStatus == "failed" ||
		stepStatus == "skipped" || stepStatus == "cancelled"

	c.JSON(consts.StatusOK, utils.H{
		"lines":    lines,
		"hasMore":  false,
		"complete": complete,
	})
}

// handleAgentLogIngestion receives log lines from the agent.
// POST /internal/logs
func (s *Server) handleAgentLogIngestion(ctx context.Context, c *app.RequestContext) {
	var req struct {
		RunID    string            `json:"runId"`
		StepName string            `json:"stepName"`
		OrgID    string            `json:"orgId"`
		Lines    []logsink.LogLine `json:"lines"`
	}
	if err := c.BindJSON(&req); err != nil {
		c.JSON(consts.StatusBadRequest, utils.H{"error": "invalid request"})
		return
	}

	if s.deps.Logs == nil {
		c.JSON(consts.StatusOK, utils.H{"status": "logs not configured"})
		return
	}

	ref := logsink.LogRef{
		OrgID:    req.OrgID,
		RunID:    req.RunID,
		StepName: req.StepName,
	}

	if err := s.deps.Logs.Write(ctx, ref, req.Lines); err != nil {
		c.JSON(consts.StatusInternalServerError, utils.H{"error": "failed to write logs"})
		return
	}

	c.JSON(consts.StatusOK, utils.H{"status": "ok", "lines": len(req.Lines)})
}
