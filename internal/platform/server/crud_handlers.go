package server

import (
	"context"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
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

	rows, err := s.deps.DB.Query(ctx, `
		SELECT t.id, t.name, t.slug,
		       COALESCE(t.source, 'internal') AS source,
		       t.idp_group,
		       COUNT(tm.user_id) AS member_count
		FROM teams t
		LEFT JOIN team_members tm ON tm.team_id = t.id
		WHERE t.org_id = $1
		GROUP BY t.id
		ORDER BY t.name
		LIMIT $2 OFFSET $3
	`, claims.OrgID, lim, off)
	if err != nil {
		apiInternal(ctx, c, "failed to list teams")
		return
	}
	defer rows.Close()

	result := []teamResponse{}
	for rows.Next() {
		var t teamResponse
		var source *string
		if err := rows.Scan(&t.ID, &t.Name, &t.Slug, &source, &t.IDPGroup, &t.MemberCount); err != nil {
			apiInternal(ctx, c, "failed to scan team")
			return
		}
		t.Source = derefString(source)
		if t.Source == "" {
			t.Source = "internal"
		}
		result = append(result, t)
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

	// Use raw query to join role and scope tables.
	rows, err := s.deps.DB.Query(ctx, `
		SELECT ak.id, ak.name,
		       COALESCE(r.slug, 'viewer') AS role,
		       COALESCE(u.email, 'system') AS created_by,
		       ak.expires_at, ak.last_used_at, ak.created_at
		FROM api_keys ak
		LEFT JOIN roles r ON r.id = ak.role_id
		LEFT JOIN users u ON u.id = ak.user_id
		WHERE ak.org_id = $1
		ORDER BY ak.created_at DESC
		LIMIT $2 OFFSET $3
	`, claims.OrgID, lim, off)
	if err != nil {
		// Fall back to sqlc query if the role_id column doesn't exist yet.
		keys, sqlcErr := s.deps.Q.ListAPIKeys(ctx, db.ListAPIKeysParams{
			OrgID: claims.OrgID, Limit: int32(lim), Offset: int32(off),
		})
		if sqlcErr != nil {
			apiInternal(ctx, c, "failed to list API keys")
			return
		}
		result := make([]apiKeyResponse, 0, len(keys))
		for _, k := range keys {
			resp := apiKeyResponse{
				ID:           k.ID,
				Name:         k.Name,
				Role:         "viewer",
				Workspaces:   []string{},
				Environments: []string{},
				CreatedBy:    "system",
				CreatedAt:    k.CreatedAt.Format(time.RFC3339),
			}
			if k.ExpiresAt != nil {
				s := k.ExpiresAt.Format(time.RFC3339)
				resp.ExpiresAt = &s
			}
			if k.LastUsedAt != nil {
				s := k.LastUsedAt.Format(time.RFC3339)
				resp.LastUsedAt = &s
			}
			result = append(result, resp)
		}
		paginatedResponse(c, result, PaginationResponse{NextCursor: nextOffsetCursor(off, lim, len(keys))})
		return
	}
	defer rows.Close()

	var result []apiKeyResponse
	for rows.Next() {
		var k apiKeyResponse
		var expiresAt, lastUsedAt *time.Time
		var createdAt time.Time

		if err := rows.Scan(&k.ID, &k.Name, &k.Role, &k.CreatedBy,
			&expiresAt, &lastUsedAt, &createdAt); err != nil {
			apiInternal(ctx, c, "failed to scan API key")
			return
		}

		k.CreatedAt = createdAt.Format(time.RFC3339)
		if expiresAt != nil {
			s := expiresAt.Format(time.RFC3339)
			k.ExpiresAt = &s
		}
		if lastUsedAt != nil {
			s := lastUsedAt.Format(time.RFC3339)
			k.LastUsedAt = &s
		}

		// Load workspace and environment scopes.
		k.Workspaces = []string{}
		k.Environments = []string{}

		if wsSlugs, wsErr := s.deps.Q.ListAPIKeyWorkspaceSlugs(ctx, k.ID); wsErr == nil {
			k.Workspaces = wsSlugs
		}

		if envSlugs, envErr := s.deps.Q.ListAPIKeyEnvironmentSlugs(ctx, k.ID); envErr == nil {
			k.Environments = envSlugs
		}

		result = append(result, k)
	}

	if result == nil {
		result = []apiKeyResponse{}
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
