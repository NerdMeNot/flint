package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
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
	rows, err := s.deps.DB.Query(ctx,
		`SELECT id, name, description, scope, is_secret, created_at
		 FROM env_variables WHERE org_id = $1 ORDER BY name`, claims.OrgID)
	if err != nil {
		apiInternal(ctx, c, "failed to list env variables")
		return
	}
	defer rows.Close()

	var result []envVariableResponse
	for rows.Next() {
		var v envVariableResponse
		var desc *string
		if err := rows.Scan(&v.ID, &v.Name, &desc, &v.Scope, &v.IsSecret, &v.CreatedAt); err != nil {
			apiInternal(ctx, c, "failed to scan env variable")
			return
		}
		v.Description = desc

		// For global variables, load the single value.
		if v.Scope == "global" && !v.IsSecret {
			var val *string
			_ = s.deps.DB.QueryRow(ctx,
				`SELECT value FROM env_variable_values
				 WHERE variable_id = $1 AND environment_id IS NULL`, v.ID).Scan(&val)
			v.Value = val
		} else if v.Scope == "global" && v.IsSecret {
			// Indicate a value exists without revealing it.
			var exists bool
			_ = s.deps.DB.QueryRow(ctx,
				`SELECT EXISTS(SELECT 1 FROM env_variable_values
				  WHERE variable_id = $1 AND environment_id IS NULL)`, v.ID).Scan(&exists)
			if exists {
				masked := "••••••••"
				v.Value = &masked
			}
		}

		result = append(result, v)
	}

	if result == nil {
		result = []envVariableResponse{}
	}
	c.JSON(consts.StatusOK, utils.H{"items": result})
}

func (s *Server) handleListEnvVariableValues(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	rows, err := s.deps.DB.Query(ctx,
		`SELECT evv.variable_id, evv.environment_id::text, evv.value, evv.updated_at,
		        ev.is_secret
		 FROM env_variable_values evv
		 JOIN env_variables ev ON ev.id = evv.variable_id
		 WHERE ev.org_id = $1
		 ORDER BY ev.name, evv.environment_id`, claims.OrgID)
	if err != nil {
		apiInternal(ctx, c, "failed to list env variable values")
		return
	}
	defer rows.Close()

	var result []envVariableValueResponse
	for rows.Next() {
		var v envVariableValueResponse
		var envID *string
		var isSecret bool
		if err := rows.Scan(&v.VariableID, &envID, &v.Value, &v.UpdatedAt, &isSecret); err != nil {
			apiInternal(ctx, c, "failed to scan env variable value")
			return
		}
		v.EnvironmentID = envID
		if isSecret {
			v.Value = "••••••••"
		}
		result = append(result, v)
	}

	if result == nil {
		result = []envVariableValueResponse{}
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

	var id string
	err := s.deps.DB.QueryRow(ctx,
		`INSERT INTO env_variables (org_id, name, description, scope, is_secret)
		 VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		claims.OrgID, req.Name, req.Description, req.Scope, req.IsSecret,
	).Scan(&id)
	if err != nil {
		apiConflict(ctx, c, "variable already exists")
		return
	}

	// For global variables, optionally set the initial value.
	if req.Scope == "global" && req.Value != nil && *req.Value != "" {
		val := *req.Value
		if req.IsSecret {
			// TODO: encrypt with secret store
			_ = val
		}
		_, _ = s.deps.DB.Exec(ctx,
			`INSERT INTO env_variable_values (variable_id, environment_id, value)
			 VALUES ($1, NULL, $2) ON CONFLICT (variable_id, environment_id) DO UPDATE SET value = $2, updated_at = now()`,
			id, val)
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
	var isSecret bool
	err := s.deps.DB.QueryRow(ctx,
		`SELECT is_secret FROM env_variables WHERE id = $1`, req.VariableID).Scan(&isSecret)
	if err != nil {
		apiNotFound(ctx, c, "variable not found")
		return
	}

	val := req.Value
	if isSecret {
		// TODO: encrypt with secret store
		_ = val
	}

	_, err = s.deps.DB.Exec(ctx,
		`INSERT INTO env_variable_values (variable_id, environment_id, value, updated_at)
		 VALUES ($1, $2, $3, now())
		 ON CONFLICT (variable_id, environment_id) DO UPDATE SET value = $3, updated_at = now()`,
		req.VariableID, req.EnvironmentID, val)
	if err != nil {
		apiInternal(ctx, c, "failed to set variable value")
		return
	}

	c.JSON(consts.StatusOK, utils.H{"status": "ok"})
}

func (s *Server) handleDeleteEnvVariable(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	if id == "" {
		apiBadRequest(ctx, c, "id is required")
		return
	}

	_, err := s.deps.DB.Exec(ctx, `DELETE FROM env_variables WHERE id = $1`, id)
	if err != nil {
		apiInternal(ctx, c, "failed to delete variable")
		return
	}

	c.JSON(consts.StatusOK, utils.H{"status": "deleted"})
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
	rows, err := s.deps.DB.Query(ctx,
		`SELECT id, name, slug, created_at FROM environments WHERE org_id = $1 ORDER BY name`,
		claims.OrgID)
	if err != nil {
		apiInternal(ctx, c, "failed to list environments")
		return
	}
	defer rows.Close()

	var result []environmentResponse
	for rows.Next() {
		var e environmentResponse
		if err := rows.Scan(&e.ID, &e.Name, &e.Slug, &e.CreatedAt); err != nil {
			apiInternal(ctx, c, "failed to scan environment")
			return
		}
		result = append(result, e)
	}

	if result == nil {
		result = []environmentResponse{}
	}
	c.JSON(consts.StatusOK, utils.H{"items": result})
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
	var id string
	err := s.deps.DB.QueryRow(ctx,
		`INSERT INTO environments (org_id, name, slug) VALUES ($1, $2, $3) RETURNING id`,
		claims.OrgID, req.Name, req.Slug,
	).Scan(&id)
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

	_, err := s.deps.DB.Exec(ctx, `DELETE FROM environments WHERE id = $1`, id)
	if err != nil {
		apiInternal(ctx, c, "failed to delete environment")
		return
	}

	c.JSON(consts.StatusOK, utils.H{"status": "deleted"})
}

func generateToken(prefix string) (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(bytes), nil
}
