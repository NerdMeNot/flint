package server

import (
	"context"
	"encoding/json"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/platform/auth"
)

// savedViewOwnerID maps the authenticated principal to the owning user's UUID.
// saved_views.owner_user_id is a FK to users(id), whereas the auth subject is the
// user's email/external id — so we resolve via the email.
func (s *Server) savedViewOwnerID(ctx context.Context, claims *auth.Claims, orgID string) (string, error) {
	u, err := s.deps.Q.GetUserByEmail(ctx, db.GetUserByEmailParams{OrgID: orgID, Email: claims.Email})
	if err != nil {
		return "", err
	}
	return u.ID, nil
}

// savedViewResponse is a user's saved view — a named navigation target (a route
// plus its URL filters/selector). Personal to the owning user.
type savedViewResponse struct {
	ID     string         `json:"id"`
	Name   string         `json:"name"`
	Route  string         `json:"route"`
	Search map[string]any `json:"search"`
}

// selectorJSON marshals a selector map for storage; nil/empty becomes "{}".
func selectorJSON(search map[string]any) []byte {
	if len(search) == 0 {
		return []byte("{}")
	}
	b, err := json.Marshal(search)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func decodeSelector(raw []byte) map[string]any {
	out := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &out)
	}
	return out
}

func (s *Server) handleListSavedViews(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	if claims == nil {
		apiUnauthorized(ctx, c, "not authenticated")
		return
	}
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to get org")
		return
	}
	ownerID, err := s.savedViewOwnerID(ctx, claims, org.ID)
	if err != nil {
		apiInternal(ctx, c, "failed to resolve user")
		return
	}
	rows, err := s.deps.Q.ListSavedViews(ctx, db.ListSavedViewsParams{
		OrgID: org.ID, OwnerUserID: ownerID,
	})
	if err != nil {
		apiInternal(ctx, c, "failed to list views")
		return
	}
	result := make([]savedViewResponse, 0, len(rows))
	for _, r := range rows {
		result = append(result, savedViewResponse{
			ID: r.ID, Name: r.Name, Route: r.Route, Search: decodeSelector(r.Selector),
		})
	}
	paginatedResponse(c, result, PaginationResponse{})
}

func (s *Server) handleCreateSavedView(ctx context.Context, c *app.RequestContext) {
	var req struct {
		Name   string         `json:"name"`
		Route  string         `json:"route"`
		Search map[string]any `json:"search"`
	}
	if err := c.BindJSON(&req); err != nil || req.Name == "" || req.Route == "" {
		apiBadRequest(ctx, c, "name and route are required")
		return
	}
	claims := claimsFromCtx(ctx)
	if claims == nil {
		apiUnauthorized(ctx, c, "not authenticated")
		return
	}
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to get org")
		return
	}
	ownerID, err := s.savedViewOwnerID(ctx, claims, org.ID)
	if err != nil {
		apiInternal(ctx, c, "failed to resolve user")
		return
	}
	id, err := s.deps.Q.CreateSavedView(ctx, db.CreateSavedViewParams{
		OrgID: org.ID, OwnerUserID: ownerID,
		Name: req.Name, Route: req.Route, Selector: selectorJSON(req.Search),
	})
	if err != nil {
		apiInternal(ctx, c, "failed to create view")
		return
	}
	search := req.Search
	if search == nil {
		search = map[string]any{}
	}
	c.JSON(consts.StatusCreated, savedViewResponse{ID: id, Name: req.Name, Route: req.Route, Search: search})
}

func (s *Server) handleDeleteSavedView(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	if claims == nil {
		apiUnauthorized(ctx, c, "not authenticated")
		return
	}
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to get org")
		return
	}
	ownerID, err := s.savedViewOwnerID(ctx, claims, org.ID)
	if err != nil {
		apiInternal(ctx, c, "failed to resolve user")
		return
	}
	rows, err := s.deps.Q.DeleteSavedView(ctx, db.DeleteSavedViewParams{
		ID: c.Param("id"), OrgID: org.ID, OwnerUserID: ownerID,
	})
	if err != nil || rows == 0 {
		apiNotFound(ctx, c, "view not found")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true})
}
