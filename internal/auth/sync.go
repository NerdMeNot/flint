package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/NerdMeNot/flint/internal/db"
	"github.com/casbin/casbin/v2"
)

// SyncUserOnLogin upserts a user, syncs team membership from IdP groups,
// and ensures Casbin assignments exist. Called on every SSO login.
func SyncUserOnLogin(ctx context.Context, q *db.Queries, enforcer *casbin.Enforcer,
	orgID string, claims *Claims, adminUsers []string, defaultRole string) (string, error) {

	// 1. Upsert user.
	name := claims.Name
	userID, err := q.UpsertUser(ctx, db.UpsertUserParams{
		OrgID:      orgID,
		Email:      claims.Email,
		ExternalID: claims.ExternalID,
		Name:       &name,
	})
	if err != nil {
		return "", fmt.Errorf("upserting user: %w", err)
	}

	// 2. Sync team membership from IdP groups.
	if err := syncTeams(ctx, q, orgID, userID, claims.Groups); err != nil {
		return "", fmt.Errorf("syncing teams: %w", err)
	}

	// 3. Ensure Casbin assignments exist.
	if err := syncCasbinAssignments(enforcer, claims.Email, claims.Groups, adminUsers, defaultRole); err != nil {
		return "", fmt.Errorf("syncing casbin assignments: %w", err)
	}

	return userID, nil
}

// syncTeams creates teams from IdP groups and manages user membership.
func syncTeams(ctx context.Context, q *db.Queries, orgID, userID string, groups []string) error {
	// Remove user from all teams first (clean slate).
	if _, err := q.RemoveUserFromAllTeams(ctx, userID); err != nil {
		return fmt.Errorf("removing stale team memberships: %w", err)
	}

	// Add user to teams matching their IdP groups.
	for _, group := range groups {
		slug := slugify(group)
		if slug == "" {
			continue
		}

		teamID, err := q.GetOrCreateTeamBySlug(ctx, db.GetOrCreateTeamBySlugParams{
			OrgID: orgID,
			Name:  group,
			Slug:  slug,
		})
		if err != nil {
			return fmt.Errorf("get/create team %q: %w", group, err)
		}

		if err := q.AddTeamMember(ctx, db.AddTeamMemberParams{
			TeamID: teamID,
			UserID: userID,
		}); err != nil {
			return fmt.Errorf("adding team member: %w", err)
		}
	}

	return nil
}

// syncCasbinAssignments ensures the user has appropriate Casbin role assignments.
func syncCasbinAssignments(enforcer *casbin.Enforcer, email string, groups []string, adminUsers []string, defaultRole string) error {
	// Admin users always get org_admin globally.
	for _, adminEmail := range adminUsers {
		if strings.EqualFold(email, adminEmail) {
			if _, err := enforcer.AddGroupingPolicy(email, "org_admin", "*"); err != nil {
				return fmt.Errorf("adding admin assignment: %w", err)
			}
			return nil // admin users don't need further assignment
		}
	}

	// Check if user has any existing Casbin assignments (from prior login or admin setup).
	roles, err := enforcer.GetRolesForUser(email)
	if err != nil {
		return fmt.Errorf("getting roles for user: %w", err)
	}

	// Also check if any of the user's IdP groups have Casbin assignments.
	// If a group has a role binding, the user inherits it via Casbin's g rules.
	hasGroupAssignment := false
	for _, group := range groups {
		groupRoles, _ := enforcer.GetRolesForUser(group)
		if len(groupRoles) > 0 {
			hasGroupAssignment = true
			break
		}
	}

	// If the user has no direct or group-inherited assignments,
	// assign the default role globally.
	if len(roles) == 0 && !hasGroupAssignment {
		if _, err := enforcer.AddGroupingPolicy(email, defaultRole, "*"); err != nil {
			return fmt.Errorf("adding default role: %w", err)
		}
	}

	return nil
}

// slugify converts a string to a URL-safe slug.
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' {
			return r
		}
		if r == ' ' || r == '_' || r == '.' || r == '@' {
			return '-'
		}
		return -1
	}, s)
	// Remove consecutive dashes.
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}
