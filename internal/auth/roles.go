package auth

import (
	"context"
	"fmt"

	"github.com/NerdMeNot/flint/internal/db"
	"github.com/casbin/casbin/v2"
)

// Permission represents a single Casbin policy entry (object + action).
type Permission struct {
	Object string `json:"object"`
	Action string `json:"action"`
}

// PredefinedRoles maps role slugs to their permission sets.
// These are seeded into Casbin as "p" policies on first boot.
var PredefinedRoles = map[string]struct {
	Name        string
	Description string
	Permissions []Permission
}{
	"org_admin": {
		Name:        "Organization Admin",
		Description: "Full access to all platform features",
		Permissions: []Permission{
			{"org", "manage"}, {"org", "read"},
			{"project", "create"}, {"project", "update"}, {"project", "archive"}, {"project", "read"},
			{"pipeline", "run"}, {"pipeline", "cancel"}, {"pipeline", "read"},
			{"gate", "approve"},
			{"secret", "create"}, {"secret", "read"}, {"secret", "delete"},
			{"runner", "manage"}, {"runner", "read"},
			{"rbac", "manage"},
			{"audit", "read"},
		},
	},
	"pipeline_admin": {
		Name:        "Pipeline Admin",
		Description: "Manage projects, pipelines, and secrets",
		Permissions: []Permission{
			{"org", "read"},
			{"project", "create"}, {"project", "update"}, {"project", "archive"}, {"project", "read"},
			{"pipeline", "run"}, {"pipeline", "cancel"}, {"pipeline", "read"},
			{"gate", "approve"},
			{"secret", "create"}, {"secret", "read"}, {"secret", "delete"},
			{"runner", "read"},
		},
	},
	"developer": {
		Name:        "Developer",
		Description: "Run pipelines, approve gates, read projects and secrets",
		Permissions: []Permission{
			{"org", "read"},
			{"project", "read"},
			{"pipeline", "run"}, {"pipeline", "cancel"}, {"pipeline", "read"},
			{"gate", "approve"},
			{"secret", "read"},
			{"runner", "read"},
		},
	},
	"viewer": {
		Name:        "Viewer",
		Description: "Read-only access to projects, pipelines, and runners",
		Permissions: []Permission{
			{"org", "read"},
			{"project", "read"},
			{"pipeline", "read"},
			{"runner", "read"},
		},
	},
}

// SeedDefaultRoles creates the 4 predefined roles and their Casbin policies
// if they don't already exist. Called on server boot. Idempotent.
func SeedDefaultRoles(ctx context.Context, q *db.Queries, enforcer *casbin.Enforcer, orgID string) error {
	for slug, def := range PredefinedRoles {
		// Create role metadata if it doesn't exist.
		exists, err := q.RoleExists(ctx, db.RoleExistsParams{OrgID: orgID, Slug: slug})
		if err != nil {
			return fmt.Errorf("checking role %s: %w", slug, err)
		}
		if !exists {
			_, err := q.CreateRole(ctx, db.CreateRoleParams{
				OrgID:       orgID,
				Name:        def.Name,
				Slug:        slug,
				Description: &def.Description,
				IsSystem:    true,
			})
			if err != nil {
				return fmt.Errorf("creating role %s: %w", slug, err)
			}
		}

		// Seed Casbin policies for this role.
		for _, perm := range def.Permissions {
			// p, <role_slug>, *, <object>, <action>
			// The "*" domain means this role definition applies globally.
			// Workspace scoping happens through "g" (grouping) rules.
			if _, err := enforcer.AddPolicy(slug, "*", perm.Object, perm.Action); err != nil {
				return fmt.Errorf("adding policy for role %s: %w", slug, err)
			}
		}
	}

	return nil
}

// SeedAdminUsers ensures each email in adminUsers has an org_admin assignment
// in Casbin (global, all workspaces). Idempotent.
func SeedAdminUsers(enforcer *casbin.Enforcer, adminUsers []string) error {
	for _, email := range adminUsers {
		// g, <email>, org_admin, *
		if _, err := enforcer.AddGroupingPolicy(email, "org_admin", "*"); err != nil {
			return fmt.Errorf("adding admin user %s: %w", email, err)
		}
	}
	return nil
}
