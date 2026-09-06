package server

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// ── Response types ────────────────────────────────────────────

type gateResponse struct {
	RunID         string  `json:"runId"`
	StepName      string  `json:"stepName"`
	Status        string  `json:"status"`
	Message       string  `json:"message"`
	ProjectName   string  `json:"projectName"`
	ProjectColour string  `json:"projectColour"`
	Workspace     string  `json:"workspace"`
	Environment   string  `json:"environment"`
	Branch        string  `json:"branch"`
	TriggeredBy   string  `json:"triggeredBy"`
	ReviewedBy    *string `json:"reviewedBy,omitempty"`
	ReviewedAt    *string `json:"reviewedAt,omitempty"`
	CreatedAt     string  `json:"createdAt"`
}

// ── Handlers ──────────────────────────────────────────────────

func (s *Server) handleListGates(ctx context.Context, c *app.RequestContext, scope WorkspaceScope) {
	// Map the UI status filter to the DB step status; default to waiting (pending).
	var dbStatus string
	switch queryString(c, "status") {
	case "", "pending":
		dbStatus = "waiting"
	case "approved":
		dbStatus = "succeeded"
	case "rejected":
		dbStatus = "failed"
	default:
		dbStatus = queryString(c, "status")
	}

	// A gate is an action on a run, so the list is narrowed to the workspaces
	// the caller can read runs in.
	if scope.Empty(nil) {
		c.JSON(consts.StatusOK, utils.H{"items": []gateResponse{}})
		return
	}

	rows, err := s.deps.Q.ListGatesByStatus(ctx, db.ListGatesByStatusParams{
		Status:     dbStatus,
		Workspaces: scope.Filter(nil),
	})
	if err != nil {
		apiInternal(ctx, c, "failed to list gates")
		return
	}

	result := make([]gateResponse, 0, len(rows))
	for _, row := range rows {
		// Map the DB status back to the UI vocabulary.
		uiStatus := row.Status
		switch row.Status {
		case "waiting":
			uiStatus = "pending"
		case "succeeded":
			uiStatus = "approved"
		case "failed":
			uiStatus = "rejected"
		}

		g := gateResponse{
			RunID:         row.RunID,
			StepName:      row.StepName,
			Status:        uiStatus,
			Message:       row.Message,
			ProjectName:   derefString(row.ProjectName),
			ProjectColour: row.ProjectColour,
			Workspace:     row.Workspace,
			Environment:   row.Environment,
			Branch:        derefString(row.Branch),
			TriggeredBy:   derefString(row.TriggeredBy),
			CreatedAt:     row.CreatedAt.Format(time.RFC3339),
		}
		if rb, ok := row.ReviewedBy.(string); ok && rb != "" {
			g.ReviewedBy = &rb
		}
		if row.ReviewedAt != nil {
			sv := row.ReviewedAt.Format(time.RFC3339)
			g.ReviewedAt = &sv
		}
		result = append(result, g)
	}

	c.JSON(consts.StatusOK, utils.H{"items": result})
}
