package server

import (
	"context"
	"encoding/json"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// ── Response types ────────────────────────────────────────────

type roleResponse struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Slug         string            `json:"slug"`
	Description  *string           `json:"description,omitempty"`
	IsSystem     bool              `json:"isSystem"`
	Permissions  []auth.Permission `json:"permissions"`
	Workspaces   []string          `json:"workspaces"`
	Environments []string          `json:"environments"`
}

type assignmentResponse struct {
	Subject string `json:"subject"`
	Role    string `json:"role"`
}

type workspaceResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description *string `json:"description,omitempty"`
	CreatedAt   string  `json:"createdAt"`
}

// ── Roles ──────────────────────────────────────────────────

func (s *Server) handleListRoles(ctx context.Context, c *app.RequestContext) {
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to get org")
		return
	}

	roles, err := s.deps.Q.ListRoles(ctx, org.ID)
	if err != nil {
		apiInternal(ctx, c, "failed to list roles")
		return
	}

	result := make([]roleResponse, 0, len(roles))
	for _, r := range roles {
		perms := s.getPermissionsForRole(r.Slug)
		if perms == nil {
			perms = []auth.Permission{}
		}

		workspaces := s.getWorkspaceScopesForRole(ctx, r.ID)
		environments := s.getEnvironmentScopesForRole(ctx, r.ID)

		result = append(result, roleResponse{
			ID:           r.ID,
			Name:         r.Name,
			Slug:         r.Slug,
			Description:  r.Description,
			IsSystem:     r.IsSystem,
			Permissions:  perms,
			Workspaces:   workspaces,
			Environments: environments,
		})
	}

	c.JSON(consts.StatusOK, utils.H{"items": result})
}

func (s *Server) handleCreateRole(ctx context.Context, c *app.RequestContext) {
	var req struct {
		Name         string            `json:"name"`
		Slug         string            `json:"slug"`
		Description  string            `json:"description"`
		Permissions  []auth.Permission `json:"permissions"`
		Workspaces   []string          `json:"workspaces"`
		Environments []string          `json:"environments"`
	}
	if err := c.BindJSON(&req); err != nil || req.Name == "" || req.Slug == "" {
		apiBadRequest(ctx, c, "name, slug, and permissions are required")
		return
	}
	if len(req.Permissions) == 0 {
		apiBadRequest(ctx, c, "at least one permission is required")
		return
	}

	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to get org")
		return
	}

	// Create role metadata.
	id, err := s.deps.Q.CreateRole(ctx, db.CreateRoleParams{
		OrgID:       org.ID,
		Name:        req.Name,
		Slug:        req.Slug,
		Description: &req.Description,
		IsSystem:    false,
	})
	if err != nil {
		apiError(ctx, c, consts.StatusConflict, "CONFLICT", "role slug already exists")
		return
	}

	// Add Casbin policies for the role.
	for _, perm := range req.Permissions {
		if _, err := s.deps.Enforcer.AddPolicy(req.Slug, "*", "*", perm.Object, perm.Action); err != nil {
			apiInternal(ctx, c, "failed to add policy")
			return
		}
	}

	// Store workspace scope rows.
	for _, wsSlug := range req.Workspaces {
		_ = s.deps.Q.InsertRoleWorkspaceScope(ctx, db.InsertRoleWorkspaceScopeParams{
			RoleID: id,
			Slug:   wsSlug,
		})
	}

	// Store environment scope rows.
	for _, envSlug := range req.Environments {
		_ = s.deps.Q.InsertRoleEnvironmentScope(ctx, db.InsertRoleEnvironmentScopeParams{
			RoleID: id,
			Slug:   envSlug,
		})
	}

	c.JSON(consts.StatusCreated, utils.H{"id": id, "slug": req.Slug})
}

func (s *Server) handleDeleteRole(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")

	// Check it's not a system role (DeleteRole has is_system = false condition).
	rows, err := s.deps.Q.DeleteRole(ctx, id)
	if err != nil || rows == 0 {
		apiBadRequest(ctx, c, "role not found or is a system role")
		return
	}

	c.JSON(consts.StatusOK, utils.H{"success": true})
}

// ── Role Assignments ──────────────────────────────────────

func (s *Server) handleListAssignments(ctx context.Context, c *app.RequestContext) {
	if s.deps.Enforcer == nil {
		c.JSON(consts.StatusOK, utils.H{"items": []any{}})
		return
	}

	// Get all "g" policies (role assignments).
	groupingPolicies, err := s.deps.Enforcer.GetGroupingPolicy()
	if err != nil {
		apiInternal(ctx, c, "failed to list assignments")
		return
	}

	assignments := make([]assignmentResponse, 0, len(groupingPolicies))
	for _, gp := range groupingPolicies {
		if len(gp) >= 2 {
			assignments = append(assignments, assignmentResponse{
				Subject: gp[0],
				Role:    gp[1],
			})
		}
	}

	c.JSON(consts.StatusOK, utils.H{"items": assignments})
}

func (s *Server) handleCreateAssignment(ctx context.Context, c *app.RequestContext) {
	var req struct {
		Subject string `json:"subject"`
		Role    string `json:"role"`
	}
	if err := c.BindJSON(&req); err != nil || req.Subject == "" || req.Role == "" {
		apiBadRequest(ctx, c, "subject and role are required")
		return
	}

	if s.deps.Enforcer == nil {
		apiInternal(ctx, c, "RBAC not configured")
		return
	}

	added, err := s.deps.Enforcer.AddGroupingPolicy(req.Subject, req.Role)
	if err != nil {
		apiInternal(ctx, c, "failed to add assignment")
		return
	}
	if !added {
		c.JSON(consts.StatusOK, utils.H{"status": "already_exists"})
		return
	}

	c.JSON(consts.StatusCreated, utils.H{"status": "created"})
}

func (s *Server) handleDeleteAssignment(ctx context.Context, c *app.RequestContext) {
	// The "id" parameter encodes the assignment as JSON.
	raw := c.Param("id")

	var req struct {
		Subject string `json:"subject"`
		Role    string `json:"role"`
	}
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		// Try query params instead.
		req.Subject = string(c.Query("subject"))
		req.Role = string(c.Query("role"))
	}

	if req.Subject == "" || req.Role == "" {
		apiBadRequest(ctx, c, "subject and role are required")
		return
	}

	if s.deps.Enforcer == nil {
		apiInternal(ctx, c, "RBAC not configured")
		return
	}

	removed, err := s.deps.Enforcer.RemoveGroupingPolicy(req.Subject, req.Role)
	if err != nil {
		apiInternal(ctx, c, "failed to remove assignment")
		return
	}
	if !removed {
		apiNotFound(ctx, c, "assignment not found")
		return
	}

	c.JSON(consts.StatusOK, utils.H{"success": true})
}

// ── Environments moved to env_variable_handlers.go ───────────

// ── Workspaces ─────────────────────────────────────────────

func (s *Server) handleListWorkspaces(ctx context.Context, c *app.RequestContext) {
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to get org")
		return
	}

	workspaces, err := s.deps.Q.ListWorkspaces(ctx, org.ID)
	if err != nil {
		apiInternal(ctx, c, "failed to list workspaces")
		return
	}

	result := make([]workspaceResponse, 0, len(workspaces))
	for _, w := range workspaces {
		result = append(result, workspaceResponse{
			ID:          w.ID,
			Name:        w.Name,
			Slug:        w.Slug,
			Description: w.Description,
			CreatedAt:   w.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	c.JSON(consts.StatusOK, utils.H{"items": result})
}

func (s *Server) handleCreateWorkspace(ctx context.Context, c *app.RequestContext) {
	var req struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
	}
	if err := c.BindJSON(&req); err != nil || req.Name == "" || req.Slug == "" {
		apiBadRequest(ctx, c, "name and slug are required")
		return
	}

	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to get org")
		return
	}

	desc := req.Description
	id, err := s.deps.Q.CreateWorkspace(ctx, db.CreateWorkspaceParams{
		OrgID:       org.ID,
		Name:        req.Name,
		Slug:        req.Slug,
		Description: &desc,
	})
	if err != nil {
		apiError(ctx, c, consts.StatusConflict, "CONFLICT", "workspace slug already exists")
		return
	}

	c.JSON(consts.StatusCreated, utils.H{"id": id, "name": req.Name, "slug": req.Slug})
}

func (s *Server) handleDeleteWorkspace(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	rows, err := s.deps.Q.DeleteWorkspace(ctx, id)
	if err != nil || rows == 0 {
		apiNotFound(ctx, c, "workspace not found")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true})
}

// ── Helpers ────────────────────────────────────────────────

// getPermissionsForRole extracts permissions for a role from Casbin policies.
func (s *Server) getPermissionsForRole(roleSlug string) []auth.Permission {
	if s.deps.Enforcer == nil {
		return nil
	}

	policies, err := s.deps.Enforcer.GetFilteredPolicy(0, roleSlug)
	if err != nil {
		return nil
	}

	perms := make([]auth.Permission, 0, len(policies))
	for _, p := range policies {
		// p = [sub, ws, env, obj, act]
		if len(p) >= 5 {
			perms = append(perms, auth.Permission{Object: p[3], Action: p[4]})
		}
	}
	return perms
}

// getWorkspaceScopesForRole loads workspace slugs scoped to a role.
func (s *Server) getWorkspaceScopesForRole(ctx context.Context, roleID string) []string {
	if s.deps.Q == nil {
		return []string{}
	}
	slugs, err := s.deps.Q.ListRoleWorkspaceSlugs(ctx, roleID)
	if err != nil {
		return []string{}
	}
	return slugs
}

// getEnvironmentScopesForRole loads environment names scoped to a role.
func (s *Server) getEnvironmentScopesForRole(ctx context.Context, roleID string) []string {
	if s.deps.Q == nil {
		return []string{}
	}
	names, err := s.deps.Q.ListRoleEnvironmentNames(ctx, roleID)
	if err != nil {
		return []string{}
	}
	return names
}
