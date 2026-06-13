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
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Slug          string  `json:"slug"`
	Description   *string `json:"description,omitempty"`
	CreatedAt     string  `json:"createdAt"`
	OwnerTeamID   *string `json:"ownerTeamId,omitempty"`
	OwnerTeamName *string `json:"ownerTeamName,omitempty"`
}

// ── Roles ──────────────────────────────────────────────────

func (s *Server) handleListRoles(ctx context.Context, c *app.RequestContext) {
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to get org")
		return
	}

	lim := parsePagination(c).Limit
	off := listOffset(c)
	roles, err := s.deps.Q.ListRoles(ctx, db.ListRolesParams{
		OrgID: org.ID, Limit: int32(lim), Offset: int32(off),
	})
	if err != nil {
		apiInternal(ctx, c, "failed to list roles")
		return
	}

	result := make([]roleResponse, 0, len(roles))
	for _, r := range roles {
		perms := s.getPermissionsForRole(ctx, r.ID)
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

	paginatedResponse(c, result, PaginationResponse{NextCursor: nextOffsetCursor(off, lim, len(roles))})
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

	// Persist the role's permissions and scope to the DB. The role_permissions /
	// role_*_scope tables are the source of truth: Casbin policies are generated
	// from them (per assignment) by auth.RegenerateForSubject, so a role created
	// here survives a policy regeneration / server restart. A new role has no
	// assignments yet, so there is nothing to materialize into Casbin until a
	// subject is assigned to it.
	for _, perm := range req.Permissions {
		if err := s.deps.Q.InsertRolePermission(ctx, db.InsertRolePermissionParams{
			RoleID: id,
			Object: perm.Object,
			Action: perm.Action,
		}); err != nil {
			apiInternal(ctx, c, "failed to persist permission")
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

	// Capture assigned subjects before deleting; the FK cascade removes their
	// role_assignments rows, so we regenerate their policies afterwards to drop the
	// now-revoked grants while keeping their other roles.
	assigned, _ := s.deps.Q.ListRoleAssignmentsByRole(ctx, id)

	// DeleteRole has an is_system = false condition, so system roles are protected.
	rows, err := s.deps.Q.DeleteRole(ctx, id)
	if err != nil || rows == 0 {
		apiBadRequest(ctx, c, "role not found or is a system role")
		return
	}

	if s.deps.Enforcer != nil {
		for _, a := range assigned {
			if err := auth.RegenerateForSubject(ctx, s.deps.Q, s.deps.DB, s.deps.Enforcer, a.Subject); err != nil {
				apiInternal(ctx, c, "failed to apply role deletion")
				return
			}
		}
	}

	c.JSON(consts.StatusOK, utils.H{"success": true})
}

// ── Role Assignments ──────────────────────────────────────

func (s *Server) handleListAssignments(ctx context.Context, c *app.RequestContext) {
	rows, err := s.deps.Q.ListAllRoleAssignmentsWithRole(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to list assignments")
		return
	}

	// Assignments come from a single ordered query; paginate the slice in memory.
	lim := parsePagination(c).Limit
	off := listOffset(c)
	var page []db.ListAllRoleAssignmentsWithRoleRow
	if off < len(rows) {
		end := off + lim
		if end > len(rows) {
			end = len(rows)
		}
		page = rows[off:end]
	}

	assignments := make([]assignmentResponse, 0, len(page))
	for _, r := range page {
		assignments = append(assignments, assignmentResponse{
			Subject: r.Subject,
			Role:    r.Role,
		})
	}

	paginatedResponse(c, assignments, PaginationResponse{NextCursor: nextOffsetCursor(off, lim, len(page))})
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

	roleID, err := s.resolveRoleID(ctx, req.Role)
	if err != nil {
		apiBadRequest(ctx, c, "unknown role")
		return
	}

	// Persist the assignment, then materialize it into Casbin from the DB. Writing
	// the role_assignments row (not just an in-memory grouping policy) is what makes
	// the assignment survive a policy regeneration / restart.
	if err := s.deps.Q.InsertRoleAssignment(ctx, db.InsertRoleAssignmentParams{
		Subject: req.Subject,
		RoleID:  roleID,
	}); err != nil {
		apiInternal(ctx, c, "failed to create assignment")
		return
	}

	if s.deps.Enforcer != nil {
		if err := auth.RegenerateForSubject(ctx, s.deps.Q, s.deps.DB, s.deps.Enforcer, req.Subject); err != nil {
			apiInternal(ctx, c, "failed to apply assignment")
			return
		}
	}

	c.JSON(consts.StatusCreated, utils.H{"status": "created"})
}

func (s *Server) handleDeleteAssignment(ctx context.Context, c *app.RequestContext) {
	// The assignment may arrive as a JSON-encoded :id path segment, as the subject
	// in the path with ?role= in the query (the web client's shape), or as
	// ?subject=&role= query params. Support all three.
	subject, role := "", ""
	raw := c.Param("id")
	var encoded struct {
		Subject string `json:"subject"`
		Role    string `json:"role"`
	}
	if err := json.Unmarshal([]byte(raw), &encoded); err == nil && encoded.Subject != "" {
		subject, role = encoded.Subject, encoded.Role
	} else {
		subject = raw
	}
	if subject == "" {
		subject = string(c.Query("subject"))
	}
	if role == "" {
		role = string(c.Query("role"))
	}

	if subject == "" || role == "" {
		apiBadRequest(ctx, c, "subject and role are required")
		return
	}

	roleID, err := s.resolveRoleID(ctx, role)
	if err != nil {
		apiNotFound(ctx, c, "assignment not found")
		return
	}

	if err := s.deps.Q.DeleteRoleAssignment(ctx, db.DeleteRoleAssignmentParams{
		Subject: subject,
		RoleID:  roleID,
	}); err != nil {
		apiInternal(ctx, c, "failed to remove assignment")
		return
	}

	if s.deps.Enforcer != nil {
		if err := auth.RegenerateForSubject(ctx, s.deps.Q, s.deps.DB, s.deps.Enforcer, subject); err != nil {
			apiInternal(ctx, c, "failed to apply assignment change")
			return
		}
	}

	c.JSON(consts.StatusOK, utils.H{"success": true})
}

// resolveRoleID maps a role slug to its id within the current org.
func (s *Server) resolveRoleID(ctx context.Context, slug string) (string, error) {
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		return "", err
	}
	role, err := s.deps.Q.GetRoleBySlug(ctx, db.GetRoleBySlugParams{OrgID: org.ID, Slug: slug})
	if err != nil {
		return "", err
	}
	return role.ID, nil
}

// ── Environments moved to env_variable_handlers.go ───────────

// ── Workspaces ─────────────────────────────────────────────

func (s *Server) handleListWorkspaces(ctx context.Context, c *app.RequestContext) {
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to get org")
		return
	}

	lim := parsePagination(c).Limit
	off := listOffset(c)
	workspaces, err := s.deps.Q.ListWorkspaces(ctx, db.ListWorkspacesParams{
		OrgID: org.ID, Limit: int32(lim), Offset: int32(off),
	})
	if err != nil {
		apiInternal(ctx, c, "failed to list workspaces")
		return
	}

	result := make([]workspaceResponse, 0, len(workspaces))
	for _, w := range workspaces {
		result = append(result, workspaceResponse{
			ID:            w.ID,
			Name:          w.Name,
			Slug:          w.Slug,
			Description:   w.Description,
			CreatedAt:     w.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
			OwnerTeamID:   w.OwnerTeamID,
			OwnerTeamName: w.OwnerTeamName,
		})
	}
	paginatedResponse(c, result, PaginationResponse{NextCursor: nextOffsetCursor(off, lim, len(workspaces))})
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

// handleSetWorkspaceOwnerTeam sets (or clears, with a null teamId) the team that
// owns a workspace.
func (s *Server) handleSetWorkspaceOwnerTeam(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	var req struct {
		TeamID *string `json:"teamId"`
	}
	if err := c.BindJSON(&req); err != nil {
		apiBadRequest(ctx, c, "invalid request")
		return
	}
	if err := s.deps.Q.SetWorkspaceOwnerTeam(ctx, db.SetWorkspaceOwnerTeamParams{
		ID: id, OwnerTeamID: req.TeamID,
	}); err != nil {
		apiInternal(ctx, c, "failed to set owning team")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true})
}

// ── Helpers ────────────────────────────────────────────────

// getPermissionsForRole loads a role's permissions from the role_permissions
// table — the source of truth from which Casbin policies are generated.
func (s *Server) getPermissionsForRole(ctx context.Context, roleID string) []auth.Permission {
	if s.deps.Q == nil {
		return nil
	}
	rows, err := s.deps.Q.ListRolePermissions(ctx, roleID)
	if err != nil {
		return nil
	}
	perms := make([]auth.Permission, 0, len(rows))
	for _, r := range rows {
		perms = append(perms, auth.Permission{Object: r.Object, Action: r.Action})
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
