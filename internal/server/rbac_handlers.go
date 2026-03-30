package server

import (
	"context"
	"encoding/json"

	"github.com/NerdMeNot/flint/internal/auth"
	"github.com/NerdMeNot/flint/internal/db"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

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

	// Enrich with permissions from Casbin.
	type roleWithPerms struct {
		ID          string            `json:"id"`
		Name        string            `json:"name"`
		Slug        string            `json:"slug"`
		Description *string           `json:"description"`
		IsSystem    bool              `json:"isSystem"`
		Permissions []auth.Permission `json:"permissions"`
	}

	result := make([]roleWithPerms, 0, len(roles))
	for _, r := range roles {
		perms := s.getPermissionsForRole(r.Slug)
		result = append(result, roleWithPerms{
			ID:          r.ID,
			Name:        r.Name,
			Slug:        r.Slug,
			Description: r.Description,
			IsSystem:    r.IsSystem,
			Permissions: perms,
		})
	}

	c.JSON(consts.StatusOK, utils.H{"roles": result})
}

func (s *Server) handleCreateRole(ctx context.Context, c *app.RequestContext) {
	var req struct {
		Name        string            `json:"name"`
		Slug        string            `json:"slug"`
		Description string            `json:"description"`
		Permissions []auth.Permission `json:"permissions"`
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
		if _, err := s.deps.Enforcer.AddPolicy(req.Slug, "*", perm.Object, perm.Action); err != nil {
			apiInternal(ctx, c, "failed to add policy")
			return
		}
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

	c.JSON(consts.StatusOK, utils.H{"status": "deleted"})
}

// ── Role Assignments ──────────────────────────────────────

func (s *Server) handleListAssignments(ctx context.Context, c *app.RequestContext) {
	if s.deps.Enforcer == nil {
		c.JSON(consts.StatusOK, utils.H{"assignments": []any{}})
		return
	}

	// Get all "g" policies (role assignments).
	groupingPolicies, err := s.deps.Enforcer.GetGroupingPolicy()
	if err != nil {
		apiInternal(ctx, c, "failed to list assignments")
		return
	}

	type assignment struct {
		Subject   string `json:"subject"`
		Role      string `json:"role"`
		Workspace string `json:"workspace"`
	}

	assignments := make([]assignment, 0, len(groupingPolicies))
	for _, gp := range groupingPolicies {
		if len(gp) >= 3 {
			assignments = append(assignments, assignment{
				Subject:   gp[0],
				Role:      gp[1],
				Workspace: gp[2],
			})
		}
	}

	c.JSON(consts.StatusOK, utils.H{"assignments": assignments})
}

func (s *Server) handleCreateAssignment(ctx context.Context, c *app.RequestContext) {
	var req struct {
		Subject   string `json:"subject"`
		Role      string `json:"role"`
		Workspace string `json:"workspace"`
	}
	if err := c.BindJSON(&req); err != nil || req.Subject == "" || req.Role == "" {
		apiBadRequest(ctx, c, "subject and role are required")
		return
	}
	if req.Workspace == "" {
		req.Workspace = "*"
	}

	if s.deps.Enforcer == nil {
		apiInternal(ctx, c, "RBAC not configured")
		return
	}

	added, err := s.deps.Enforcer.AddGroupingPolicy(req.Subject, req.Role, req.Workspace)
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
		Subject   string `json:"subject"`
		Role      string `json:"role"`
		Workspace string `json:"workspace"`
	}
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		// Try query params instead.
		req.Subject = string(c.Query("subject"))
		req.Role = string(c.Query("role"))
		req.Workspace = string(c.Query("workspace"))
	}

	if req.Subject == "" || req.Role == "" {
		apiBadRequest(ctx, c, "subject and role are required")
		return
	}
	if req.Workspace == "" {
		req.Workspace = "*"
	}

	if s.deps.Enforcer == nil {
		apiInternal(ctx, c, "RBAC not configured")
		return
	}

	removed, err := s.deps.Enforcer.RemoveGroupingPolicy(req.Subject, req.Role, req.Workspace)
	if err != nil {
		apiInternal(ctx, c, "failed to remove assignment")
		return
	}
	if !removed {
		apiNotFound(ctx, c, "assignment not found")
		return
	}

	c.JSON(consts.StatusOK, utils.H{"status": "deleted"})
}

// ── Protected Environments ─────────────────────────────────

func (s *Server) handleListEnvironments(ctx context.Context, c *app.RequestContext) {
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to get org")
		return
	}

	envs, err := s.deps.Q.ListProtectedEnvironments(ctx, org.ID)
	if err != nil {
		apiInternal(ctx, c, "failed to list environments")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"environments": envs})
}

func (s *Server) handleCreateEnvironment(ctx context.Context, c *app.RequestContext) {
	var req struct {
		Name           string   `json:"name"`
		MinRole        string   `json:"minRole"`
		Approvers      []string `json:"approvers"`
		DeployBranches []string `json:"deployBranches"`
		DeployWindow   any      `json:"deployWindow"`
	}
	if err := c.BindJSON(&req); err != nil || req.Name == "" {
		apiBadRequest(ctx, c, "name is required")
		return
	}
	if req.MinRole == "" {
		req.MinRole = "pipeline_admin"
	}
	if req.Approvers == nil {
		req.Approvers = []string{}
	}
	if req.DeployBranches == nil {
		req.DeployBranches = []string{}
	}

	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to get org")
		return
	}

	var deployWindow []byte
	if req.DeployWindow != nil {
		deployWindow, _ = json.Marshal(req.DeployWindow)
	}

	id, err := s.deps.Q.CreateProtectedEnvironment(ctx, db.CreateProtectedEnvironmentParams{
		OrgID:          org.ID,
		Name:           req.Name,
		MinRole:        req.MinRole,
		Approvers:      req.Approvers,
		DeployBranches: req.DeployBranches,
		DeployWindow:   deployWindow,
	})
	if err != nil {
		apiError(ctx, c, consts.StatusConflict, "CONFLICT", "environment already exists")
		return
	}

	c.JSON(consts.StatusCreated, utils.H{"id": id, "name": req.Name})
}

func (s *Server) handleUpdateEnvironment(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	var req struct {
		MinRole        string   `json:"minRole"`
		Approvers      []string `json:"approvers"`
		DeployBranches []string `json:"deployBranches"`
		DeployWindow   any      `json:"deployWindow"`
	}
	if err := c.BindJSON(&req); err != nil {
		apiBadRequest(ctx, c, "invalid request body")
		return
	}

	var deployWindow []byte
	if req.DeployWindow != nil {
		deployWindow, _ = json.Marshal(req.DeployWindow)
	}

	rows, err := s.deps.Q.UpdateProtectedEnvironment(ctx, db.UpdateProtectedEnvironmentParams{
		ID:             id,
		MinRole:        req.MinRole,
		Approvers:      req.Approvers,
		DeployBranches: req.DeployBranches,
		DeployWindow:   deployWindow,
	})
	if err != nil || rows == 0 {
		apiNotFound(ctx, c, "environment not found")
		return
	}

	c.JSON(consts.StatusOK, utils.H{"status": "updated"})
}

func (s *Server) handleDeleteEnvironment(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	rows, err := s.deps.Q.DeleteProtectedEnvironment(ctx, id)
	if err != nil || rows == 0 {
		apiNotFound(ctx, c, "environment not found")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"status": "deleted"})
}

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
	c.JSON(consts.StatusOK, utils.H{"workspaces": workspaces})
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

	c.JSON(consts.StatusCreated, utils.H{"id": id, "slug": req.Slug})
}

func (s *Server) handleDeleteWorkspace(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	rows, err := s.deps.Q.DeleteWorkspace(ctx, id)
	if err != nil || rows == 0 {
		apiNotFound(ctx, c, "workspace not found")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"status": "deleted"})
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
		if len(p) >= 4 {
			perms = append(perms, auth.Permission{Object: p[2], Action: p[3]})
		}
	}
	return perms
}
