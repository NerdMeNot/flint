package server

import (
	"context"
	"strconv"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
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

func (s *Server) handleListGates(ctx context.Context, c *app.RequestContext) {
	statusFilter := queryString(c, "status")

	// Use a raw query that enriches gates with workspace/environment info.
	query := `
		SELECT s.name AS step_name,
		       COALESCE(s.step_def->'gate'->>'message', '') AS message,
		       pr.id AS run_id, pr.trigger_ref AS branch, pr.triggered_by,
		       p.display_name AS project_name, p.colour AS project_colour,
		       COALESCE(w.slug, '') AS workspace,
		       COALESCE(s.step_def->'gate'->>'environment', '') AS environment,
		       s.status, s.created_at,
		       s.result->>'approvedBy' AS reviewed_by,
		       s.finished_at AS reviewed_at
		FROM steps s
		JOIN workflows wf ON s.workflow_id = wf.id
		JOIN pipeline_runs pr ON wf.run_id = pr.id
		JOIN projects p ON pr.project_id = p.id
		LEFT JOIN workspaces w ON w.id = p.workspace_id
		WHERE s.exec_type = 'gate'
	`

	args := []any{}
	argIdx := 1

	if statusFilter != "" {
		// Map UI status names to DB statuses.
		dbStatus := statusFilter
		switch statusFilter {
		case "pending":
			dbStatus = "waiting"
		case "approved":
			dbStatus = "succeeded"
		case "rejected":
			dbStatus = "failed"
		}
		query += ` AND s.status = $` + strconv.Itoa(argIdx)
		args = append(args, dbStatus)
		argIdx++
	} else {
		// Default: show waiting gates.
		query += ` AND s.status = 'waiting'`
	}

	query += ` ORDER BY s.created_at ASC LIMIT 50`

	rows, err := s.deps.DB.Query(ctx, query, args...)
	if err != nil {
		// Fall back to sqlc query if the raw query fails (e.g., missing columns).
		gates, sqlcErr := s.deps.Q.ListPendingGates(ctx)
		if sqlcErr != nil {
			apiInternal(ctx, c, "failed to list gates")
			return
		}
		result := make([]gateResponse, 0, len(gates))
		for _, g := range gates {
			msg := ""
			if g.Message != nil {
				if s, ok := g.Message.(string); ok {
					msg = s
				}
			}
			result = append(result, gateResponse{
				RunID:         g.RunID,
				StepName:      g.StepName,
				Status:        "pending",
				Message:       msg,
				ProjectName:   derefString(g.ProjectName),
				ProjectColour: g.ProjectColour,
				Branch:        derefString(g.Branch),
				TriggeredBy:   derefString(g.TriggeredBy),
				CreatedAt:     g.CreatedAt.Format(time.RFC3339),
			})
		}
		c.JSON(consts.StatusOK, utils.H{"items": result})
		return
	}
	defer rows.Close()

	var result []gateResponse
	for rows.Next() {
		var g gateResponse
		var branch, triggeredBy, projectName, reviewedBy *string
		var reviewedAt *time.Time
		var createdAt time.Time
		var dbStatus string

		if err := rows.Scan(
			&g.StepName, &g.Message,
			&g.RunID, &branch, &triggeredBy,
			&projectName, &g.ProjectColour,
			&g.Workspace, &g.Environment,
			&dbStatus, &createdAt,
			&reviewedBy, &reviewedAt,
		); err != nil {
			apiInternal(ctx, c, "failed to scan gate")
			return
		}

		// Map DB status back to UI status.
		switch dbStatus {
		case "waiting":
			g.Status = "pending"
		case "succeeded":
			g.Status = "approved"
		case "failed":
			g.Status = "rejected"
		default:
			g.Status = dbStatus
		}

		g.Branch = derefString(branch)
		g.TriggeredBy = derefString(triggeredBy)
		g.ProjectName = derefString(projectName)
		g.CreatedAt = createdAt.Format(time.RFC3339)
		g.ReviewedBy = reviewedBy
		if reviewedAt != nil {
			s := reviewedAt.Format(time.RFC3339)
			g.ReviewedAt = &s
		}

		result = append(result, g)
	}

	if result == nil {
		result = []gateResponse{}
	}
	c.JSON(consts.StatusOK, utils.H{"items": result})
}
