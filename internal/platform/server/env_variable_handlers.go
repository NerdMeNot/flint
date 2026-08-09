package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// ── Environment Variable Types ───────────────────────────────

type envVariableResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Scope       string  `json:"scope"`
	IsSecret    bool    `json:"isSecret"`
	Value       *string `json:"value,omitempty"`
	CreatedAt   string  `json:"createdAt"`
}

type envVariableValueResponse struct {
	VariableID    string  `json:"variableId"`
	EnvironmentID *string `json:"environmentId,omitempty"`
	Value         string  `json:"value"`
	UpdatedAt     string  `json:"updatedAt"`
}

// ── Handlers ─────────────────────────────────────────────────

func (s *Server) handleListEnvVariables(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	lim := parsePagination(c).Limit
	off := listOffset(c)
	rows, err := s.deps.Q.ListEnvVariables(ctx, db.ListEnvVariablesParams{
		OrgID: claims.OrgID, Limit: int32(lim), Offset: int32(off),
	})
	if err != nil {
		apiInternal(ctx, c, "failed to list env variables")
		return
	}

	result := make([]envVariableResponse, 0, len(rows))
	for _, row := range rows {
		v := envVariableResponse{
			ID:          row.ID,
			Name:        row.Name,
			Description: row.Description,
			Scope:       row.Scope,
			IsSecret:    row.IsSecret,
			CreatedAt:   row.CreatedAt.Format("2006-01-02T15:04:05Z"),
		}

		// For global variables, load the single value.
		if v.Scope == "global" && !v.IsSecret {
			val, err := s.deps.Q.GetGlobalVariableValue(ctx, v.ID)
			if err == nil {
				v.Value = &val
			}
		} else if v.Scope == "global" && v.IsSecret {
			// Indicate a value exists without revealing it.
			exists, err := s.deps.Q.GlobalVariableValueExists(ctx, v.ID)
			if err == nil && exists {
				masked := "••••••••"
				v.Value = &masked
			}
		}

		result = append(result, v)
	}

	paginatedResponse(c, result, PaginationResponse{NextCursor: nextOffsetCursor(off, lim, len(rows))})
}

func (s *Server) handleListEnvVariableValues(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	rows, err := s.deps.Q.ListEnvVariableValues(ctx, claims.OrgID)
	if err != nil {
		apiInternal(ctx, c, "failed to list env variable values")
		return
	}

	result := make([]envVariableValueResponse, 0, len(rows))
	for _, row := range rows {
		v := envVariableValueResponse{
			VariableID: row.VariableID,
			Value:      row.Value,
			UpdatedAt:  row.UpdatedAt.Format("2006-01-02T15:04:05Z"),
		}
		if row.EnvironmentID != "" {
			envID := row.EnvironmentID
			v.EnvironmentID = &envID
		}
		if row.IsSecret {
			v.Value = "••••••••"
		}
		result = append(result, v)
	}

	c.JSON(consts.StatusOK, utils.H{"items": result})
}

func (s *Server) handleCreateEnvVariable(ctx context.Context, c *app.RequestContext) {
	var req struct {
		Name        string  `json:"name"`
		Description *string `json:"description,omitempty"`
		Scope       string  `json:"scope"`
		IsSecret    bool    `json:"isSecret"`
		Value       *string `json:"value,omitempty"`
	}
	if err := c.BindJSON(&req); err != nil || req.Name == "" {
		apiBadRequest(ctx, c, "name is required")
		return
	}
	if req.Scope != "global" && req.Scope != "environment" {
		apiBadRequest(ctx, c, "scope must be 'global' or 'environment'")
		return
	}

	claims := claimsFromCtx(ctx)

	id, err := s.deps.Q.CreateEnvVariable(ctx, db.CreateEnvVariableParams{
		OrgID:       claims.OrgID,
		Name:        req.Name,
		Description: req.Description,
		Scope:       req.Scope,
		IsSecret:    req.IsSecret,
	})
	if err != nil {
		apiConflict(ctx, c, "variable already exists")
		return
	}

	// For global variables, optionally set the initial value. Secret values are
	// encrypted at rest (envelope, server master key); plaintext is stored as-is.
	if req.Scope == "global" && req.Value != nil && *req.Value != "" {
		if req.IsSecret {
			enc, err := s.encryptWithMasterKey([]byte(*req.Value))
			if err != nil {
				apiInternal(ctx, c, "failed to encrypt secret value")
				return
			}
			_ = s.deps.Q.UpsertSecretEnvVariableValue(ctx, db.UpsertSecretEnvVariableValueParams{
				VariableID:    id,
				EnvironmentID: nil,
				ValueEnc:      enc,
			})
		} else {
			_ = s.deps.Q.UpsertEnvVariableValue(ctx, db.UpsertEnvVariableValueParams{
				VariableID:    id,
				EnvironmentID: nil,
				Value:         *req.Value,
			})
		}
	}

	c.JSON(consts.StatusCreated, utils.H{"id": id, "name": req.Name, "scope": req.Scope})
}

func (s *Server) handleSetEnvVariableValue(ctx context.Context, c *app.RequestContext) {
	var req struct {
		VariableID    string  `json:"variableId"`
		EnvironmentID *string `json:"environmentId,omitempty"`
		Value         string  `json:"value"`
	}
	if err := c.BindJSON(&req); err != nil || req.VariableID == "" || req.Value == "" {
		apiBadRequest(ctx, c, "variableId and value are required")
		return
	}

	// Check if the variable is a secret — if so, encrypt.
	isSecret, err := s.deps.Q.IsEnvVariableSecret(ctx, req.VariableID)
	if err != nil {
		apiNotFound(ctx, c, "variable not found")
		return
	}

	if isSecret {
		enc, err := s.encryptWithMasterKey([]byte(req.Value))
		if err != nil {
			apiInternal(ctx, c, "failed to encrypt secret value")
			return
		}
		if err := s.deps.Q.UpsertSecretEnvVariableValue(ctx, db.UpsertSecretEnvVariableValueParams{
			VariableID:    req.VariableID,
			EnvironmentID: req.EnvironmentID,
			ValueEnc:      enc,
		}); err != nil {
			apiInternal(ctx, c, "failed to set variable value")
			return
		}
	} else {
		if err := s.deps.Q.UpsertEnvVariableValue(ctx, db.UpsertEnvVariableValueParams{
			VariableID:    req.VariableID,
			EnvironmentID: req.EnvironmentID,
			Value:         req.Value,
		}); err != nil {
			apiInternal(ctx, c, "failed to set variable value")
			return
		}
	}

	c.JSON(consts.StatusOK, utils.H{"status": "ok"})
}

func (s *Server) handleDeleteEnvVariable(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	if id == "" {
		apiBadRequest(ctx, c, "id is required")
		return
	}

	err := s.deps.Q.DeleteEnvVariable(ctx, id)
	if err != nil {
		apiInternal(ctx, c, "failed to delete variable")
		return
	}

	c.JSON(consts.StatusOK, utils.H{"success": true})
}

// ── Simplified Environments ──────────────────────────────────

type environmentResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	CreatedAt string `json:"createdAt"`
}

func (s *Server) handleListEnvironments(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	lim := parsePagination(c).Limit
	off := listOffset(c)
	rows, err := s.deps.Q.ListEnvironments(ctx, db.ListEnvironmentsParams{
		OrgID: claims.OrgID, Limit: int32(lim), Offset: int32(off),
	})
	if err != nil {
		apiInternal(ctx, c, "failed to list environments")
		return
	}

	result := make([]environmentResponse, 0, len(rows))
	for _, row := range rows {
		result = append(result, environmentResponse{
			ID:        row.ID,
			Name:      row.Name,
			Slug:      row.Slug,
			CreatedAt: row.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}

	paginatedResponse(c, result, PaginationResponse{NextCursor: nextOffsetCursor(off, lim, len(rows))})
}

func (s *Server) handleCreateEnvironment(ctx context.Context, c *app.RequestContext) {
	var req struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := c.BindJSON(&req); err != nil || req.Name == "" || req.Slug == "" {
		apiBadRequest(ctx, c, "name and slug required")
		return
	}

	claims := claimsFromCtx(ctx)
	id, err := s.deps.Q.CreateEnvironment(ctx, db.CreateEnvironmentParams{
		OrgID: claims.OrgID,
		Name:  req.Name,
		Slug:  req.Slug,
	})
	if err != nil {
		apiConflict(ctx, c, "environment already exists")
		return
	}

	c.JSON(consts.StatusCreated, environmentResponse{ID: id, Name: req.Name, Slug: req.Slug})
}

func (s *Server) handleDeleteEnvironment(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	if id == "" {
		apiBadRequest(ctx, c, "id is required")
		return
	}

	err := s.deps.Q.DeleteEnvironment(ctx, id)
	if err != nil {
		apiInternal(ctx, c, "failed to delete environment")
		return
	}

	c.JSON(consts.StatusOK, utils.H{"success": true})
}

func generateToken(prefix string) (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(bytes), nil
}
