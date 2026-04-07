package server

import (
	"context"

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

	// Get the user ID from email.
	var userID string
	err := s.deps.DB.QueryRow(ctx,
		`SELECT id FROM users WHERE org_id = $1 AND email = $2`,
		claims.OrgID, claims.Email).Scan(&userID)
	if err != nil {
		apiNotFound(ctx, c, "user not found")
		return
	}

	rows, err := s.deps.DB.Query(ctx,
		`SELECT id, name, expires_at, last_used_at, created_at
		 FROM personal_tokens WHERE user_id = $1
		 ORDER BY created_at DESC`, userID)
	if err != nil {
		apiInternal(ctx, c, "failed to list personal tokens")
		return
	}
	defer rows.Close()

	var result []personalTokenResponse
	for rows.Next() {
		var t personalTokenResponse
		if err := rows.Scan(&t.ID, &t.Name, &t.ExpiresAt, &t.LastUsedAt, &t.CreatedAt); err != nil {
			apiInternal(ctx, c, "failed to scan personal token")
			return
		}
		result = append(result, t)
	}

	if result == nil {
		result = []personalTokenResponse{}
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

	// Get the user ID.
	var userID string
	err := s.deps.DB.QueryRow(ctx,
		`SELECT id FROM users WHERE org_id = $1 AND email = $2`,
		claims.OrgID, claims.Email).Scan(&userID)
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

	var id string
	err = s.deps.DB.QueryRow(ctx,
		`INSERT INTO personal_tokens (user_id, name, token_hash, expires_at)
		 VALUES ($1, $2, $3, $4) RETURNING id`,
		userID, req.Name, string(hash), req.ExpiresAt,
	).Scan(&id)
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

	_, err := s.deps.DB.Exec(ctx, `DELETE FROM personal_tokens WHERE id = $1`, id)
	if err != nil {
		apiInternal(ctx, c, "failed to revoke personal token")
		return
	}

	c.JSON(consts.StatusOK, utils.H{"status": "revoked"})
}
