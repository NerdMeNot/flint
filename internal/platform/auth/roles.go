package auth

import (
	"context"
	"fmt"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// SystemRoleDefinition describes a system role to be seeded on boot.
type SystemRoleDefinition struct {
	Name        string
	Slug        string
	Description string
	Permissions []Permission
}

// SystemRoles are created at platform boot. They are always unscoped
// (apply to all workspaces and environments) and cannot be modified.
var SystemRoles = []SystemRoleDefinition{
	{
		Name:        "Admin",
		Slug:        RoleAdmin,
		Description: "Full platform and CI access",
		Permissions: []Permission{
			{ObjWildcard, ActWildcard},
		},
	},
	{
		Name:        "Developer",
		Slug:        RoleDeveloper,
		Description: "Trigger and manage CI runs, read admin resources",
		Permissions: []Permission{
			// Admin — read-only.
			{ObjEnvironment, ActRead},
			{ObjRunner, ActRead},
			{ObjSecret, ActRead},
			// CI — full dev access.
			{ObjProject, ActRead},
			{ObjProject, ActWrite},
			{ObjRun, ActRead},
			{ObjRun, ActTrigger},
			{ObjRun, ActCancel},
		},
	},
	{
		Name:        "Viewer",
		Slug:        RoleViewer,
		Description: "Read-only access across the platform",
		Permissions: []Permission{
			// Admin — read-only.
			{ObjWorkspace, ActRead},
			{ObjTeam, ActRead},
			{ObjEnvironment, ActRead},
			{ObjRunner, ActRead},
			// CI — read-only.
			{ObjProject, ActRead},
			{ObjRun, ActRead},
		},
	},
	{
		Name:        "Platform Manager",
		Slug:        RolePlatformManager,
		Description: "Full admin access without CI write permissions",
		Permissions: []Permission{
			// Admin — full manage.
			{ObjWorkspace, ActRead}, {ObjWorkspace, ActManage},
			{ObjTeam, ActRead}, {ObjTeam, ActManage},
			{ObjEnvironment, ActRead}, {ObjEnvironment, ActManage},
			{ObjRunner, ActRead}, {ObjRunner, ActManage},
			{ObjConnection, ActRead}, {ObjConnection, ActManage},
			{ObjAPIKey, ActRead}, {ObjAPIKey, ActManage},
			{ObjSecret, ActRead}, {ObjSecret, ActManage},
			{ObjRole, ActRead}, {ObjRole, ActManage},
			{ObjAudit, ActRead},
			// CI — read-only.
			{ObjProject, ActRead},
			{ObjRun, ActRead},
		},
	},
}

// SeedSystemRoles ensures all system roles exist in the database with
// correct permissions. Idempotent — skips roles that already exist.
// Does NOT touch Casbin directly; call RegeneratePolicies after.
func SeedSystemRoles(ctx context.Context, q db.Querier, orgID string) error {
	for _, def := range SystemRoles {
		// Check if role exists.
		exists, err := q.RoleExists(ctx, db.RoleExistsParams{
			OrgID: orgID,
			Slug:  def.Slug,
		})
		if err != nil {
			return fmt.Errorf("checking role %s: %w", def.Slug, err)
		}

		if exists {
			continue
		}

		// Create role.
		desc := def.Description
		roleID, err := q.CreateRole(ctx, db.CreateRoleParams{
			OrgID:       orgID,
			Name:        def.Name,
			Slug:        def.Slug,
			Description: &desc,
			IsSystem:    true,
		})
		if err != nil {
			return fmt.Errorf("creating role %s: %w", def.Slug, err)
		}

		// Insert permissions.
		for _, perm := range def.Permissions {
			if err := q.InsertRolePermission(ctx, db.InsertRolePermissionParams{
				RoleID: roleID,
				Object: perm.Object,
				Action: perm.Action,
			}); err != nil {
				return fmt.Errorf("inserting permission %s for role %s: %w", perm.Key(), def.Slug, err)
			}
		}
	}

	return nil
}

// SeedAdminUsers ensures each email has an assignment to the admin role.
// Does NOT touch Casbin directly; call RegenerateForSubject after.
func SeedAdminUsers(ctx context.Context, q db.Querier, orgID string, adminEmails []string) error {
	// Find the admin role ID.
	role, err := q.GetRoleBySlug(ctx, db.GetRoleBySlugParams{
		OrgID: orgID,
		Slug:  RoleAdmin,
	})
	if err != nil {
		return fmt.Errorf("finding admin role: %w", err)
	}

	for _, email := range adminEmails {
		if err := q.InsertRoleAssignment(ctx, db.InsertRoleAssignmentParams{
			Subject: email,
			RoleID:  role.ID,
		}); err != nil {
			return fmt.Errorf("assigning admin to %s: %w", email, err)
		}
	}

	return nil
}
