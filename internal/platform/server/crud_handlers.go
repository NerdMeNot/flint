package server

import (
	"context"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"golang.org/x/crypto/bcrypt"
)

// ── Response types ───────────────────────────────────────────

type teamResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Source      string  `json:"source"`
	IDPGroup    *string `json:"idpGroup,omitempty"`
	MemberCount int     `json:"memberCount"`
}

type userResponse struct {
	ID        string  `json:"id"`
	Email     string  `json:"email"`
	Name      *string `json:"name,omitempty"`
	AvatarUrl *string `json:"avatarUrl,omitempty"`
}

type apiKeyResponse struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Role         string   `json:"role"`
	Workspaces   []string `json:"workspaces"`
	Environments []string `json:"environments"`
	ExpiresAt    *string  `json:"expiresAt,omitempty"`
	LastUsedAt   *string  `json:"lastUsedAt,omitempty"`
	CreatedBy    string   `json:"createdBy"`
	CreatedAt    string   `json:"createdAt"`
}

type forgeConnectionResponse struct {
	ID          string `json:"id"`
	ForgeType   string `json:"forgeType"`
	DisplayName string `json:"displayName"`
	CreatedAt   string `json:"createdAt"`
}

type auditEntryResponse struct {
	ID           string  `json:"id"`
	UserID       *string `json:"userId,omitempty"`
	Action       string  `json:"action"`
	ResourceType string  `json:"resourceType"`
	ResourceID   *string `json:"resourceId,omitempty"`
	Metadata     any     `json:"metadata,omitempty"`
	IPAddress    *string `json:"ipAddress,omitempty"`
	CreatedAt    string  `json:"createdAt"`
}

// ── Teams ─────────────────────────────────────────────────

func (s *Server) handleListTeams(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	lim := parsePagination(c).Limit
	off := listOffset(c)

	rows, err := s.deps.Q.ListTeamsPaged(ctx, db.ListTeamsPagedParams{
		OrgID:  claims.OrgID,
		Limit:  int32(lim),
		Offset: int32(off),
	})
	if err != nil {
		apiInternal(ctx, c, "failed to list teams")
		return
	}

	result := make([]teamResponse, 0, len(rows))
	for _, t := range rows {
		tr := teamResponse{
			ID:          t.ID,
			Name:        t.Name,
			Slug:        t.Slug,
			Source:      t.Source,
			IDPGroup:    t.IdpGroup,
			MemberCount: int(t.MemberCount),
		}
		if tr.Source == "" {
			tr.Source = "internal"
		}
		result = append(result, tr)
	}

	paginatedResponse(c, result, PaginationResponse{NextCursor: nextOffsetCursor(off, lim, len(result))})
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
	c.JSON(consts.StatusCreated, utils.H{"id": id, "name": req.Name, "slug": req.Slug, "memberCount": 0})
}

func (s *Server) handleDeleteTeam(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	n, err := s.deps.Q.DeleteTeam(ctx, id)
	if err != nil || n == 0 {
		apiNotFound(ctx, c, "team not found")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true})
}

// handleAddTeamMembers adds one or more users to a team (internal membership).
func (s *Server) handleAddTeamMembers(ctx context.Context, c *app.RequestContext) {
	teamID := c.Param("id")
	var req struct {
		UserIDs []string `json:"userIds"`
	}
	if c.BindJSON(&req) != nil || len(req.UserIDs) == 0 {
		apiBadRequest(ctx, c, "userIds required")
		return
	}
	for _, uid := range req.UserIDs {
		if err := s.deps.Q.AddTeamMember(ctx, db.AddTeamMemberParams{TeamID: teamID, UserID: uid}); err != nil {
			apiInternal(ctx, c, "failed to add team member")
			return
		}
	}
	c.JSON(consts.StatusOK, utils.H{"success": true, "added": len(req.UserIDs)})
}

// handleRemoveTeamMember removes a single user from a team.
func (s *Server) handleRemoveTeamMember(ctx context.Context, c *app.RequestContext) {
	n, err := s.deps.Q.RemoveTeamMember(ctx, db.RemoveTeamMemberParams{
		TeamID: c.Param("id"), UserID: c.Param("userId"),
	})
	if err != nil || n == 0 {
		apiNotFound(ctx, c, "team member not found")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true})
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
		result := make([]userResponse, 0, len(users))
		for _, u := range users {
			result = append(result, userResponse{
				ID:        u.ID,
				Email:     u.Email,
				Name:      u.Name,
				AvatarUrl: u.AvatarUrl,
			})
		}
		c.JSON(consts.StatusOK, utils.H{"items": result})
		return
	}

	lim := parsePagination(c).Limit
	off := listOffset(c)
	users, err := s.deps.Q.ListUsers(ctx, db.ListUsersParams{
		OrgID: claims.OrgID, Limit: int32(lim), Offset: int32(off),
	})
	if err != nil {
		apiInternal(ctx, c, "failed to list users")
		return
	}
	result := make([]userResponse, 0, len(users))
	for _, u := range users {
		result = append(result, userResponse{
			ID:        u.ID,
			Email:     u.Email,
			Name:      u.Name,
			AvatarUrl: u.AvatarUrl,
		})
	}
	paginatedResponse(c, result, PaginationResponse{NextCursor: nextOffsetCursor(off, lim, len(users))})
}

// handleCreateUser provisions a local (email + password) user and assigns a role.
// Flint normally provisions users via SSO or the `flint admin create-user` CLI;
// this powers the Users page's create form for local-auth setups. When no password
// is supplied one is generated and returned ONCE so the admin can share it.
func (s *Server) handleCreateUser(ctx context.Context, c *app.RequestContext) {
	var req struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Role     string `json:"role"`
		Password string `json:"password"`
	}
	if c.BindJSON(&req) != nil || req.Email == "" {
		apiBadRequest(ctx, c, "email is required")
		return
	}
	claims := claimsFromCtx(ctx)

	password := req.Password
	generated := false
	if password == "" {
		password = auth.GenerateRandomPassword()
		generated = true
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		apiInternal(ctx, c, "failed to hash password")
		return
	}
	var namePtr *string
	if req.Name != "" {
		namePtr = &req.Name
	}
	userID, err := s.deps.Q.CreateLocalUser(ctx, db.CreateLocalUserParams{
		OrgID: claims.OrgID, Email: req.Email, Name: namePtr, PasswordHash: &hash,
	})
	if err != nil {
		apiConflict(ctx, c, "user already exists")
		return
	}

	role := req.Role
	if role == "" {
		role = s.deps.Config.Auth.DefaultRoleOrFallback()
	}
	if roleRow, rerr := s.deps.Q.GetRoleBySlug(ctx, db.GetRoleBySlugParams{OrgID: claims.OrgID, Slug: role}); rerr == nil {
		_ = s.deps.Q.InsertRoleAssignment(ctx, db.InsertRoleAssignmentParams{Subject: req.Email, RoleID: roleRow.ID})
		if s.deps.Enforcer != nil {
			_ = auth.RegenerateForSubject(ctx, s.deps.Q, s.deps.DB, s.deps.Enforcer, req.Email)
		}
	}

	resp := utils.H{"id": userID, "email": req.Email, "name": req.Name, "role": role}
	if generated {
		resp["generatedPassword"] = password
	}
	c.JSON(consts.StatusCreated, resp)
}

// ── Forge Connections ─────────────────────────────────────

func (s *Server) handleListForgeConnections(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	lim := parsePagination(c).Limit
	off := listOffset(c)
	conns, err := s.deps.Q.ListForgeConnections(ctx, db.ListForgeConnectionsParams{
		OrgID: claims.OrgID, Limit: int32(lim), Offset: int32(off),
	})
	if err != nil {
		apiInternal(ctx, c, "failed to list forge connections")
		return
	}
	result := make([]forgeConnectionResponse, 0, len(conns))
	for _, fc := range conns {
		result = append(result, forgeConnectionResponse{
			ID:          fc.ID,
			ForgeType:   fc.ForgeType,
			DisplayName: fc.DisplayName,
			CreatedAt:   fc.CreatedAt.Format(time.RFC3339),
		})
	}
	paginatedResponse(c, result, PaginationResponse{NextCursor: nextOffsetCursor(off, lim, len(conns))})
}

// ── API Keys ──────────────────────────────────────────────

func (s *Server) handleListAPIKeys(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	lim := parsePagination(c).Limit
	off := listOffset(c)

	rows, err := s.deps.Q.ListAPIKeysDetailed(ctx, db.ListAPIKeysDetailedParams{
		OrgID:  claims.OrgID,
		Limit:  int32(lim),
		Offset: int32(off),
	})
	if err != nil {
		apiInternal(ctx, c, "failed to list API keys")
		return
	}

	result := make([]apiKeyResponse, 0, len(rows))
	for _, row := range rows {
		k := apiKeyResponse{
			ID:           row.ID,
			Name:         row.Name,
			Role:         row.Role,
			CreatedBy:    row.CreatedBy,
			CreatedAt:    row.CreatedAt.Format(time.RFC3339),
			Workspaces:   []string{},
			Environments: []string{},
		}
		if row.ExpiresAt != nil {
			s := row.ExpiresAt.Format(time.RFC3339)
			k.ExpiresAt = &s
		}
		if row.LastUsedAt != nil {
			s := row.LastUsedAt.Format(time.RFC3339)
			k.LastUsedAt = &s
		}
		if wsSlugs, e := s.deps.Q.ListAPIKeyWorkspaceSlugs(ctx, k.ID); e == nil {
			k.Workspaces = wsSlugs
		}
		if envSlugs, e := s.deps.Q.ListAPIKeyEnvironmentSlugs(ctx, k.ID); e == nil {
			k.Environments = envSlugs
		}
		result = append(result, k)
	}

	paginatedResponse(c, result, PaginationResponse{NextCursor: nextOffsetCursor(off, lim, len(result))})
}

func (s *Server) handleCreateAPIKey(ctx context.Context, c *app.RequestContext) {
	var req struct {
		Name         string   `json:"name"`
		Role         string   `json:"role"`
		Workspaces   []string `json:"workspaces"`
		Environments []string `json:"environments"`
		ExpiresAt    *string  `json:"expiresAt,omitempty"`
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

	// Try creating with role_id if supported, fall back to scopes-based.
	id, err := s.deps.Q.CreateAPIKey(ctx, db.CreateAPIKeyParams{
		OrgID:   claims.OrgID,
		UserID:  &claims.Subject,
		Name:    req.Name,
		KeyHash: string(hash),
		Scopes:  []string{},
	})
	if err != nil {
		apiInternal(ctx, c, "failed to create API key")
		return
	}

	// TODO: insert workspace/environment scope rows when tables exist.

	c.JSON(consts.StatusCreated, utils.H{
		"id":           id,
		"name":         req.Name,
		"token":        rawKey,
		"role":         req.Role,
		"workspaces":   req.Workspaces,
		"environments": req.Environments,
		"createdBy":    claims.Email,
		"createdAt":    time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleDeleteAPIKey(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	n, err := s.deps.Q.DeleteAPIKey(ctx, id)
	if err != nil || n == 0 {
		apiNotFound(ctx, c, "API key not found")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true})
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

	result := make([]auditEntryResponse, 0, len(entries))
	for _, e := range entries {
		result = append(result, auditEntryResponse{
			ID:           e.ID,
			UserID:       e.UserID,
			Action:       e.Action,
			ResourceType: e.ResourceType,
			ResourceID:   e.ResourceID,
			IPAddress:    e.IpAddress,
			CreatedAt:    e.CreatedAt.Format(time.RFC3339),
		})
	}

	var nextCursor string
	if hasMore && len(result) > 0 {
		last := result[len(result)-1]
		nextCursor = encodeCursor(last.ID, last.CreatedAt)
	}

	resp := utils.H{"items": result}
	if nextCursor != "" {
		resp["nextCursor"] = nextCursor
	}
	c.JSON(consts.StatusOK, resp)
}
