package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/casbin/casbin/v2"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// SyncUserOnLogin upserts a user, syncs team membership from IdP groups,
// and ensures role assignments exist in the DB. Called on every SSO login.
//
// Role assignments are stored in the role_assignments table. Casbin policies
// are regenerated from DB state after sync.
func SyncUserOnLogin(ctx context.Context, q db.Querier, pool db.Pool, enforcer casbin.IEnforcer,
	orgID string, claims *Claims, adminUsers []string, defaultRoleSlug string, strictGroups bool) (string, error) {

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

	// 3. Reconcile IdP-derived role assignments from group→role mappings.
	if err := reconcileGroupRoles(ctx, q, orgID, claims.Email, claims.Groups); err != nil {
		return "", fmt.Errorf("reconciling group roles: %w", err)
	}

	// 4. Ensure baseline role assignments (admin users; default role unless strict).
	if err := syncRoleAssignments(ctx, q, orgID, claims.Email, adminUsers, defaultRoleSlug, strictGroups); err != nil {
		return "", fmt.Errorf("syncing role assignments: %w", err)
	}

	// 5. Regenerate Casbin policies for this user.
	if err := RegenerateForSubject(ctx, q, pool, enforcer, claims.Email); err != nil {
		return "", fmt.Errorf("regenerating policies: %w", err)
	}

	// 6. Regenerate grouping rules — step 2 may have changed which teams the
	// user belongs to, and roles can be granted to a team subject.
	if err := RegenerateGroupingForUser(ctx, pool, enforcer, claims.Email); err != nil {
		return "", fmt.Errorf("regenerating grouping: %w", err)
	}

	return userID, nil
}

// reconcileGroupRoles makes a subject's IdP-sourced role assignments match the
// org's group→role mappings for the subject's current IdP groups. It is additive
// and reversible: roles for groups the user is still in are granted (source='idp'),
// and 'idp' grants for groups the user has left are removed. Manual ('internal')
// assignments are never touched.
func reconcileGroupRoles(ctx context.Context, q db.Querier, orgID, subject string, groups []string) error {
	// Desired role IDs from the mappings that match the user's current groups.
	desired := make(map[string]bool)
	if len(groups) > 0 {
		roleIDs, err := q.ListRoleIDsForGroups(ctx, db.ListRoleIDsForGroupsParams{OrgID: orgID, Groups: groups})
		if err != nil {
			return fmt.Errorf("listing role IDs for groups: %w", err)
		}
		for _, id := range roleIDs {
			desired[id] = true
		}
	}

	// Current IdP-sourced role IDs for this subject.
	current, err := q.ListIdpRoleAssignmentRoleIDs(ctx, subject)
	if err != nil {
		return fmt.Errorf("listing current idp role assignments: %w", err)
	}

	toGrant, toRevoke := diffRoleAssignments(desired, current)
	for _, roleID := range toGrant {
		if err := q.InsertIdpRoleAssignment(ctx, db.InsertIdpRoleAssignmentParams{Subject: subject, RoleID: roleID}); err != nil {
			return fmt.Errorf("inserting idp role assignment: %w", err)
		}
	}
	for _, roleID := range toRevoke {
		if err := q.DeleteIdpRoleAssignment(ctx, db.DeleteIdpRoleAssignmentParams{Subject: subject, RoleID: roleID}); err != nil {
			return fmt.Errorf("deleting idp role assignment: %w", err)
		}
	}
	return nil
}

// diffRoleAssignments computes which role IDs to grant (in desired but not
// current) and which to revoke (current but no longer desired).
func diffRoleAssignments(desired map[string]bool, current []string) (grant, revoke []string) {
	currentSet := make(map[string]bool, len(current))
	for _, id := range current {
		currentSet[id] = true
		if !desired[id] {
			revoke = append(revoke, id)
		}
	}
	for id := range desired {
		if !currentSet[id] {
			grant = append(grant, id)
		}
	}
	return grant, revoke
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

// syncRoleAssignments ensures the user has appropriate baseline role assignments.
// Admin users always get the admin role. Other users get the default role only
// if they have no existing assignments AND strict group mode is off — under
// strict mode, group→role mappings are the sole source of access (deny-by-default).
func syncRoleAssignments(ctx context.Context, q db.Querier,
	orgID, email string, adminUsers []string, defaultRoleSlug string, strictGroups bool) error {

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

	// Under strict group mode, do not grant a fallback role — access is governed
	// entirely by group→role mappings (which reconcileGroupRoles already applied).
	if strictGroups {
		return nil
	}

	// Check if user already has any assignments (manual or group-derived).
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
