package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/casbin/casbin/v2"
)

// SyncUserOnLogin upserts a user, syncs team membership from IdP groups,
// and ensures role assignments exist in the DB. Called on every SSO login.
//
// Role assignments are stored in the role_assignments table. Casbin policies
// are regenerated from DB state after sync.
func SyncUserOnLogin(ctx context.Context, q db.Querier, pool db.Pool, enforcer casbin.IEnforcer,
	orgID string, claims *Claims, adminUsers []string, defaultRoleSlug string) (string, error) {

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

	// 3. Ensure role assignments exist in the DB.
	if err := syncRoleAssignments(ctx, q, orgID, claims.Email, adminUsers, defaultRoleSlug); err != nil {
		return "", fmt.Errorf("syncing role assignments: %w", err)
	}

	// 4. Regenerate Casbin policies for this user.
	if err := RegenerateForSubject(ctx, q, pool, enforcer, claims.Email); err != nil {
		return "", fmt.Errorf("regenerating policies: %w", err)
	}

	return userID, nil
}

// syncTeams syncs team membership from IdP groups using additive logic.
// Only touches teams with source='idp'. Manually-assigned teams (source='internal')
// are never modified.
func syncTeams(ctx context.Context, q db.Querier, orgID, userID string, groups []string) error {
	// Build set of desired IdP group slugs.
	desired := make(map[string]string, len(groups)) // slug → group name
	for _, group := range groups {
		slug := slugify(group)
		if slug != "" {
			desired[slug] = group
		}
	}

	// Get current IdP-sourced team memberships for this user.
	currentTeams, err := q.ListUserIdpTeams(ctx, userID)
	if err != nil {
		return fmt.Errorf("listing current IdP teams: %w", err)
	}

	// Build set of current IdP team slugs.
	current := make(map[string]string, len(currentTeams)) // slug → team_id
	for _, t := range currentTeams {
		current[t.Slug] = t.ID
	}

	// Add user to new IdP teams.
	for slug, group := range desired {
		if _, exists := current[slug]; exists {
			continue // already a member
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

	// Remove from IdP teams the user is no longer in.
	for slug, teamID := range current {
		if _, stillMember := desired[slug]; !stillMember {
			if _, err := q.RemoveTeamMember(ctx, db.RemoveTeamMemberParams{
				TeamID: teamID,
				UserID: userID,
			}); err != nil {
				return fmt.Errorf("remove from team %s: %w", slug, err)
			}
		}
	}

	return nil
}

// syncRoleAssignments ensures the user has appropriate role assignments.
// Admin users get the admin role. Other users get the default role if they
// have no existing assignments.
func syncRoleAssignments(ctx context.Context, q db.Querier,
	orgID, email string, adminUsers []string, defaultRoleSlug string) error {

	// Check if this user is an admin.
	for _, adminEmail := range adminUsers {
		if strings.EqualFold(email, adminEmail) {
			// Ensure admin role assignment exists.
			role, err := q.GetRoleBySlug(ctx, db.GetRoleBySlugParams{
				OrgID: orgID,
				Slug:  RoleAdmin,
			})
			if err != nil {
				return fmt.Errorf("finding admin role: %w", err)
			}
			if err := q.InsertRoleAssignment(ctx, db.InsertRoleAssignmentParams{
				Subject: email,
				RoleID:  role.ID,
			}); err != nil {
				return fmt.Errorf("assigning admin role: %w", err)
			}
			return nil
		}
	}

	// Check if user already has any assignments.
	count, err := q.CountRoleAssignments(ctx, email)
	if err != nil {
		return fmt.Errorf("checking existing assignments: %w", err)
	}

	// If no assignments, assign the default role.
	if count == 0 {
		role, err := q.GetRoleBySlug(ctx, db.GetRoleBySlugParams{
			OrgID: orgID,
			Slug:  defaultRoleSlug,
		})
		if err != nil {
			return fmt.Errorf("finding default role %s: %w", defaultRoleSlug, err)
		}
		if err := q.InsertRoleAssignment(ctx, db.InsertRoleAssignmentParams{
			Subject: email,
			RoleID:  role.ID,
		}); err != nil {
			return fmt.Errorf("assigning default role: %w", err)
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
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}
