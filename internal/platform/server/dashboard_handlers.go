package server

import (
	"context"
	"fmt"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

func (s *Server) handleDashboardSummary(ctx context.Context, c *app.RequestContext) {
	projects, err := s.deps.Q.GetDashboardSummary(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to load dashboard")
		return
	}

	totalProjects, _ := s.deps.Q.CountActiveProjects(ctx)
	runningPipelines, _ := s.deps.Q.CountRunningPipelines(ctx)
	pendingGates, _ := s.deps.Q.CountPendingGates(ctx)

	c.JSON(consts.StatusOK, utils.H{
		"projects": projects,
		"stats": utils.H{
			"totalProjects":    totalProjects,
			"runningPipelines": runningPipelines,
			"pendingGates":     pendingGates,
		},
	})
}

func (s *Server) handleDashboardActivity(ctx context.Context, c *app.RequestContext) {
	limit := int32(20)
	if l := queryString(c, "limit"); l != "" {
		if n, err := parseInt(l); err == nil && n > 0 && n <= 100 {
			limit = int32(n)
		}
	}

	runs, err := s.deps.Q.GetDashboardActivity(ctx, limit)
	if err != nil {
		apiInternal(ctx, c, "failed to load activity")
		return
	}

	c.JSON(consts.StatusOK, utils.H{"runs": runs})
}

func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}
