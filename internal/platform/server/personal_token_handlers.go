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

// ── Personal Token Types ─────────────────────────────────────

type personalTokenResponse struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	ExpiresAt  *string `json:"expiresAt,omitempty"`
	LastUsedAt *string `json:"lastUsedAt,omitempty"`
	CreatedAt  string  `json:"createdAt"`
}

type personalTokenCreatedResponse struct {
	personalTokenResponse
	Token string `json:"token"` // shown once only
}

// ── Handlers ─────────────────────────────────────────────────

func (s *Server) handleListPersonalTokens(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)

	user, err := s.deps.Q.GetUserByEmail(ctx, db.GetUserByEmailParams{OrgID: claims.OrgID, Email: claims.Email})
	if err != nil {
		apiNotFound(ctx, c, "user not found")
		return
	}

	rows, err := s.deps.Q.ListPersonalTokensByUser(ctx, user.ID)
	if err != nil {
		apiInternal(ctx, c, "failed to list personal tokens")
		return
	}

	result := make([]personalTokenResponse, 0, len(rows))
	for _, t := range rows {
		result = append(result, personalTokenResponse{
			ID:         t.ID,
			Name:       t.Name,
			ExpiresAt:  formatTimePtrOpt(t.ExpiresAt),
			LastUsedAt: formatTimePtrOpt(t.LastUsedAt),
			CreatedAt:  t.CreatedAt.Format(time.RFC3339),
		})
	}
	c.JSON(consts.StatusOK, utils.H{"items": result})
}

func (s *Server) handleCreatePersonalToken(ctx context.Context, c *app.RequestContext) {
	var req struct {
		Name      string  `json:"name"`
		ExpiresAt *string `json:"expiresAt,omitempty"`
	}
	if err := c.BindJSON(&req); err != nil || req.Name == "" {
		apiBadRequest(ctx, c, "name is required")
		return
	}

	claims := claimsFromCtx(ctx)

	user, err := s.deps.Q.GetUserByEmail(ctx, db.GetUserByEmailParams{OrgID: claims.OrgID, Email: claims.Email})
	if err != nil {
		apiNotFound(ctx, c, "user not found")
		return
	}

	// Generate token.
	rawToken, err := generateToken("flint_pat_")
	if err != nil {
		apiInternal(ctx, c, "failed to generate token")
		return
	}

	// Hash for storage.
	hash, err := bcrypt.GenerateFromPassword([]byte(rawToken), bcrypt.DefaultCost)
	if err != nil {
		apiInternal(ctx, c, "failed to hash token")
		return
	}

	var expiresAt *time.Time
	if req.ExpiresAt != nil && *req.ExpiresAt != "" {
		if t, perr := time.Parse(time.RFC3339, *req.ExpiresAt); perr == nil {
			expiresAt = &t
		}
	}

	id, err := s.deps.Q.CreatePersonalToken(ctx, db.CreatePersonalTokenParams{
		UserID:    user.ID,
		Name:      req.Name,
		TokenHash: string(hash),
		ExpiresAt: expiresAt,
	})
	if err != nil {
		apiInternal(ctx, c, "failed to create personal token")
		return
	}

	c.JSON(consts.StatusCreated, personalTokenCreatedResponse{
		personalTokenResponse: personalTokenResponse{
			ID:        id,
			Name:      req.Name,
			ExpiresAt: req.ExpiresAt,
		},
		Token: rawToken,
	})
}

func (s *Server) handleRevokePersonalToken(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	if id == "" {
		apiBadRequest(ctx, c, "id is required")
		return
	}

	if err := s.deps.Q.DeletePersonalToken(ctx, id); err != nil {
		apiInternal(ctx, c, "failed to revoke personal token")
		return
	}

	c.JSON(consts.StatusOK, utils.H{"success": true})
}
