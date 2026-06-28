package server

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// capability describes one product surface for the unified UI.
type capability struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
	Status  string `json:"status"` // "enabled" | "coming_soon" | "disabled"
}

// handleCapabilities reports which product sections the UI should render. It is
// the single source of truth for the unified app's top-level navigation: CI and
// Workflows are gated by config; Load Testing is "coming soon" (no backend yet).
func (s *Server) handleCapabilities(ctx context.Context, c *app.RequestContext) {
	p := s.deps.Config.Products
	c.JSON(consts.StatusOK, utils.H{"products": []capability{
		{ID: "ci", Name: "CI", Enabled: p.CIEnabled(), Status: capabilityStatus(p.CIEnabled(), false)},
		{ID: "workflows", Name: "Workflows", Enabled: p.WorkflowsEnabled(), Status: capabilityStatus(p.WorkflowsEnabled(), false)},
		{ID: "loadtest", Name: "Load Testing", Enabled: false, Status: capabilityStatus(false, true)},
	}})
}

// handleMeta reports the backend's data mode. In demo deployments (the local sim
// stack: seeded data + simulated step execution) the UI surfaces a banner so demo
// state is never mistaken for production.
func (s *Server) handleMeta(ctx context.Context, c *app.RequestContext) {
	mode := "live"
	if s.deps.Demo {
		mode = "demo"
	}
	c.JSON(consts.StatusOK, utils.H{"mode": mode})
}

func capabilityStatus(enabled, comingSoon bool) string {
	switch {
	case enabled:
		return "enabled"
	case comingSoon:
		return "coming_soon"
	default:
		return "disabled"
	}
}
