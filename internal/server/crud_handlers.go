package server

import (
	"context"

	"github.com/NerdMeNot/flint/internal/db"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"golang.org/x/crypto/bcrypt"
)

// ── Teams ─────────────────────────────────────────────────

func (s *Server) handleListTeams(ctx context.Context, c *app.RequestContext) {
	teams, err := s.deps.Q.ListTeams(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to list teams")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"data": teams})
}

func (s *Server) handleCreateTeam(ctx context.Context, c *app.RequestContext) {
	var req struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if c.BindJSON(&req) != nil || req.Name == "" || req.Slug == "" {
		apiBadRequest(ctx, c, "name and slug required")
		return
	}
	claims := claimsFromCtx(ctx)
	id, err := s.deps.Q.CreateTeam(ctx, db.CreateTeamParams{
		OrgID: claims.OrgID, Name: req.Name, Slug: req.Slug,
	})
	if err != nil {
		apiConflict(ctx, c, "team already exists")
		return
	}
	c.JSON(consts.StatusCreated, utils.H{"id": id, "name": req.Name, "slug": req.Slug})
}

func (s *Server) handleDeleteTeam(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	n, err := s.deps.Q.DeleteTeam(ctx, id)
	if err != nil || n == 0 {
		apiNotFound(ctx, c, "team not found")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"status": "deleted"})
}

// ── Users ─────────────────────────────────────────────────

func (s *Server) handleListUsers(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	search := queryString(c, "search")

	if search != "" {
		users, err := s.deps.Q.SearchUsers(ctx, db.SearchUsersParams{
			OrgID: claims.OrgID,
			Email: "%" + search + "%",
		})
		if err != nil {
			apiInternal(ctx, c, "failed to search users")
			return
		}
		c.JSON(consts.StatusOK, utils.H{"data": users})
		return
	}

	users, err := s.deps.Q.ListUsers(ctx, claims.OrgID)
	if err != nil {
		apiInternal(ctx, c, "failed to list users")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"data": users})
}

// ── Forge Connections ─────────────────────────────────────

func (s *Server) handleListForgeConnections(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	conns, err := s.deps.Q.ListForgeConnections(ctx, claims.OrgID)
	if err != nil {
		apiInternal(ctx, c, "failed to list forge connections")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"data": conns})
}

// ── API Keys ──────────────────────────────────────────────

func (s *Server) handleListAPIKeys(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	keys, err := s.deps.Q.ListAPIKeys(ctx, claims.OrgID)
	if err != nil {
		apiInternal(ctx, c, "failed to list API keys")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"data": keys})
}

func (s *Server) handleCreateAPIKey(ctx context.Context, c *app.RequestContext) {
	var req struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	if c.BindJSON(&req) != nil || req.Name == "" {
		apiBadRequest(ctx, c, "name is required")
		return
	}

	rawKey := "flint_k_" + generateSecureCode(24)
	hash, err := bcrypt.GenerateFromPassword([]byte(rawKey), bcrypt.DefaultCost)
	if err != nil {
		apiInternal(ctx, c, "failed to hash key")
		return
	}

	claims := claimsFromCtx(ctx)
	id, err := s.deps.Q.CreateAPIKey(ctx, db.CreateAPIKeyParams{
		OrgID:   claims.OrgID,
		UserID:  &claims.Subject,
		Name:    req.Name,
		KeyHash: string(hash),
		Scopes:  req.Scopes,
	})
	if err != nil {
		apiInternal(ctx, c, "failed to create API key")
		return
	}

	c.JSON(consts.StatusCreated, utils.H{
		"id": id, "name": req.Name, "key": rawKey, "scopes": req.Scopes,
	})
}

func (s *Server) handleDeleteAPIKey(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	n, err := s.deps.Q.DeleteAPIKey(ctx, id)
	if err != nil || n == 0 {
		apiNotFound(ctx, c, "API key not found")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"status": "revoked"})
}

// ── Audit Log ─────────────────────────────────────────────

func (s *Server) handleListAuditLog(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	p := parsePagination(c)
	action := queryString(c, "action")

	var actionPtr *string
	if action != "" {
		actionPtr = &action
	}

	entries, err := s.deps.Q.ListAuditLog(ctx, db.ListAuditLogParams{
		OrgID:  claims.OrgID,
		Action: actionPtr,
		Limit:  int32(p.Limit + 1),
	})
	if err != nil {
		apiInternal(ctx, c, "failed to list audit log")
		return
	}

	hasMore := len(entries) > p.Limit
	if hasMore {
		entries = entries[:p.Limit]
	}

	paginatedResponse(c, entries, PaginationResponse{HasMore: hasMore})
}

// ── Modules ───────────────────────────────────────────────

func (s *Server) handleListModules(ctx context.Context, c *app.RequestContext) {
	modules, err := s.deps.Q.ListModules(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to list modules")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"data": modules})
}

// ── Org Secrets ───────────────────────────────────────────

func (s *Server) handleListOrgSecrets(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	secrets, err := s.deps.Q.ListOrgSecrets(ctx, claims.OrgID)
	if err != nil {
		apiInternal(ctx, c, "failed to list org secrets")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"data": secrets})
}
