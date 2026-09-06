package server

import (
	"context"
	"math"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/NerdMeNot/flint/internal/core/db"
)

func (s *Server) getOrg(ctx context.Context, c *app.RequestContext) {
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiNotFound(ctx, c, "no org configured")
		return
	}
	c.JSON(consts.StatusOK, utils.H{
		"id":                      org.ID,
		"name":                    org.Name,
		"slug":                    org.Slug,
		"concurrencyLimit":        org.ConcurrencyLimit,
		"requireProjectWorkspace": org.RequireProjectWorkspace,
	})
}

// handleSetOrgPolicy updates org-level governance policies. Currently just the
// "require a workspace on every project" switch.
func (s *Server) handleSetOrgPolicy(ctx context.Context, c *app.RequestContext) {
	var req struct {
		RequireProjectWorkspace bool `json:"requireProjectWorkspace"`
	}
	if err := c.BindJSON(&req); err != nil {
		apiBadRequest(ctx, c, "invalid request body")
		return
	}
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiNotFound(ctx, c, "no org configured")
		return
	}
	if err := s.deps.Q.SetOrgRequireProjectWorkspace(ctx, db.SetOrgRequireProjectWorkspaceParams{
		ID: org.ID, Require: req.RequireProjectWorkspace,
	}); err != nil {
		apiInternal(ctx, c, "failed to update org policy")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true})
}

// ── Teams -- get by ID ───────────────────────────────────────

func (s *Server) handleStats(ctx context.Context, c *app.RequestContext) {
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to get org")
		return
	}

	stats, err := s.deps.Q.GetRunStats(ctx, org.ID)
	if err != nil {
		apiInternal(ctx, c, "failed to compute run stats")
		return
	}

	activeProjects, _ := s.deps.Q.CountActiveProjects(ctx)
	pendingGates, _ := s.deps.Q.CountPendingGates(ctx)

	// Percent (0-100) to match the UI, which renders the value with a "%" suffix.
	successRate := 0.0
	if stats.TotalRuns > 0 {
		successRate = math.Round(float64(stats.SuccessRuns) / float64(stats.TotalRuns) * 100)
	}

	c.JSON(consts.StatusOK, utils.H{
		"totalRuns":      stats.TotalRuns,
		"successRate":    successRate,
		"pendingGates":   pendingGates,
		"activeProjects": activeProjects,
		"runsToday":      stats.RunsToday,
		"avgDuration":    (time.Duration(stats.AvgDurationMs) * time.Millisecond).String(),
	})
}

func (s *Server) handleSearch(ctx context.Context, c *app.RequestContext, scope WorkspaceScope) {
	q := string(c.Query("q"))
	if q == "" {
		c.JSON(consts.StatusOK, utils.H{"projects": []any{}, "runs": []any{}})
		return
	}

	claims := claimsFromCtx(ctx)
	pattern := "%" + q + "%"

	// Search spans the same rows the list routes serve, so it takes the same
	// restriction: a scope-limited caller must not be able to discover projects
	// and runs through search that the lists would hide.
	if scope.Empty(nil) {
		c.JSON(consts.StatusOK, utils.H{"projects": []any{}, "runs": []any{}})
		return
	}
	permitted := scope.Filter(nil)

	projects := []utils.H{}
	if rows, err := s.deps.Q.SearchProjects(ctx, db.SearchProjectsParams{
		OrgID: claims.OrgID, Pattern: &pattern, Workspaces: permitted,
	}); err == nil {
		for _, p := range rows {
			projects = append(projects, utils.H{"id": p.ID, "name": p.Name, "repo": p.RepoPath, "colour": p.Colour})
		}
	}

	runs := []utils.H{}
	if rows, err := s.deps.Q.SearchRuns(ctx, db.SearchRunsParams{
		OrgID: claims.OrgID, Pattern: &pattern, Workspaces: permitted,
	}); err == nil {
		for _, r := range rows {
			h := utils.H{"id": r.ID, "status": r.Status, "projectName": r.ProjectName, "projectColour": r.ProjectColour}
			if r.Branch != nil {
				h["branch"] = *r.Branch
			}
			if r.CommitSha != nil {
				h["commitSha"] = *r.CommitSha
			}
			runs = append(runs, h)
		}
	}

	c.JSON(consts.StatusOK, utils.H{"projects": projects, "runs": runs})
}

// ── Projects ──────────────────────────────────────────────────
