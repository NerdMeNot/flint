package server

import (
	"context"
	"encoding/json"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// handleListWebhooks lists all webhooks for a project.
// GET /api/v1/projects/:id/webhooks
func (s *Server) handleListWebhooks(ctx context.Context, c *app.RequestContext) {
	projectID := c.Param("id")

	hooks, err := s.deps.Q.ListProjectWebhooks(ctx, projectID)
	if err != nil {
		apiInternal(ctx, c, "failed to list webhooks")
		return
	}

	// Mask secrets in the response.
	type webhookResponse struct {
		ID        string          `json:"id"`
		URL       string          `json:"url"`
		HasSecret bool            `json:"hasSecret"`
		Events    json.RawMessage `json:"events"`
		IsActive  bool            `json:"isActive"`
		CreatedAt string          `json:"createdAt"`
	}

	result := make([]webhookResponse, 0, len(hooks))
	for _, h := range hooks {
		result = append(result, webhookResponse{
			ID:        h.ID,
			URL:       h.Url,
			HasSecret: h.Secret != "",
			Events:    h.Events,
			IsActive:  h.IsActive,
			CreatedAt: h.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	c.JSON(consts.StatusOK, utils.H{"webhooks": result})
}

// handleCreateWebhook creates a new webhook for a project.
// POST /api/v1/projects/:id/webhooks
func (s *Server) handleCreateWebhook(ctx context.Context, c *app.RequestContext) {
	projectID := c.Param("id")

	var req struct {
		URL    string   `json:"url"`
		Secret string   `json:"secret"`
		Events []string `json:"events"`
	}
	if err := c.BindJSON(&req); err != nil || req.URL == "" {
		apiBadRequest(ctx, c, "url is required")
		return
	}

	if len(req.Events) == 0 {
		req.Events = []string{"run.completed", "run.failed"}
	}
	eventsJSON, _ := json.Marshal(req.Events)

	id, err := s.deps.Q.CreateWebhook(ctx, db.CreateWebhookParams{
		ProjectID: projectID,
		Url:       req.URL,
		Secret:    req.Secret,
		Events:    eventsJSON,
	})
	if err != nil {
		apiInternal(ctx, c, "failed to create webhook")
		return
	}

	c.JSON(consts.StatusCreated, utils.H{"id": id})
}

// handleDeleteWebhook deletes a webhook.
// DELETE /api/v1/projects/:id/webhooks/:webhookId
func (s *Server) handleDeleteWebhook(ctx context.Context, c *app.RequestContext) {
	projectID := c.Param("id")
	webhookID := c.Param("webhookId")

	err := s.deps.Q.DeleteWebhook(ctx, db.DeleteWebhookParams{
		ID:        webhookID,
		ProjectID: projectID,
	})
	if err != nil {
		apiInternal(ctx, c, "failed to delete webhook")
		return
	}

	c.JSON(consts.StatusOK, utils.H{"success": true})
}
