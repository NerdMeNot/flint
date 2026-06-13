package server

import (
	"context"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// tagKeyResponse is a curated tag key in the registry (governs a `key:value`
// tag namespace — label, optional allowed values, display colour).
type tagKeyResponse struct {
	ID            string   `json:"id"`
	Key           string   `json:"key"`
	Label         string   `json:"label"`
	AllowedValues []string `json:"allowedValues"`
	Color         string   `json:"color"`
}

func (s *Server) handleListTagKeys(ctx context.Context, c *app.RequestContext) {
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to get org")
		return
	}
	rows, err := s.deps.Q.ListTagKeys(ctx, org.ID)
	if err != nil {
		apiInternal(ctx, c, "failed to list tag keys")
		return
	}
	result := make([]tagKeyResponse, 0, len(rows))
	for _, r := range rows {
		result = append(result, tagKeyResponse{
			ID: r.ID, Key: r.Key, Label: r.Label,
			AllowedValues: r.AllowedValues, Color: r.Color,
		})
	}
	paginatedResponse(c, result, PaginationResponse{})
}

func (s *Server) handleCreateTagKey(ctx context.Context, c *app.RequestContext) {
	var req struct {
		Key           string   `json:"key"`
		Label         string   `json:"label"`
		AllowedValues []string `json:"allowedValues"`
		Color         string   `json:"color"`
	}
	if err := c.BindJSON(&req); err != nil || req.Key == "" || req.Label == "" {
		apiBadRequest(ctx, c, "key and label are required")
		return
	}
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to get org")
		return
	}
	color := req.Color
	if color == "" {
		color = "#6366f1"
	}
	id, err := s.deps.Q.CreateTagKey(ctx, db.CreateTagKeyParams{
		OrgID: org.ID, Key: req.Key, Label: req.Label,
		AllowedValues: req.AllowedValues, Color: color,
	})
	if err != nil {
		apiError(ctx, c, consts.StatusConflict, "CONFLICT", "tag key already exists")
		return
	}
	c.JSON(consts.StatusCreated, utils.H{"id": id, "key": req.Key, "label": req.Label})
}

func (s *Server) handleUpdateTagKey(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	var req struct {
		Label         string   `json:"label"`
		AllowedValues []string `json:"allowedValues"`
		Color         string   `json:"color"`
	}
	if err := c.BindJSON(&req); err != nil {
		apiBadRequest(ctx, c, "invalid request")
		return
	}
	if err := s.deps.Q.UpdateTagKey(ctx, db.UpdateTagKeyParams{
		ID: id, Label: req.Label, AllowedValues: req.AllowedValues, Color: req.Color,
	}); err != nil {
		apiInternal(ctx, c, "failed to update tag key")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true})
}

func (s *Server) handleDeleteTagKey(ctx context.Context, c *app.RequestContext) {
	rows, err := s.deps.Q.DeleteTagKey(ctx, c.Param("id"))
	if err != nil || rows == 0 {
		apiNotFound(ctx, c, "tag key not found")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true})
}
