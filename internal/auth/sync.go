package auth

import (
	"context"
	"fmt"
	"strings"

	"github.com/NerdMeNot/flint/internal/db"
	"github.com/casbin/casbin/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SyncUserOnLogin upserts a user, syncs team membership from IdP groups,
// and ensures role assignments exist in the DB. Called on every SSO login.
//
// Role assignments are stored in the role_assignments table. Casbin policies
// are regenerated from DB state after sync.
func SyncUserOnLogin(ctx context.Context, q *db.Queries, pool *pgxpool.Pool, enforcer *casbin.Enforcer,
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
	if err := syncRoleAssignments(ctx, pool, orgID, claims.Email, adminUsers, defaultRoleSlug); err != nil {
		return "", fmt.Errorf("syncing role assignments: %w", err)
	}

	// 4. Regenerate Casbin policies for this user.
	if err := RegenerateForSubject(ctx, pool, enforcer, claims.Email); err != nil {
		return "", fmt.Errorf("regenerating policies: %w", err)
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

// syncRoleAssignments ensures the user has appropriate role assignments.
// Admin users get the admin role. Other users get the default role if they
// have no existing assignments.
func syncRoleAssignments(ctx context.Context, pool *pgxpool.Pool,
	orgID, email string, adminUsers []string, defaultRoleSlug string) error {

	// Check if this user is an admin.
	for _, adminEmail := range adminUsers {
		if strings.EqualFold(email, adminEmail) {
			// Ensure admin role assignment exists.
			_, err := pool.Exec(ctx,
				`INSERT INTO role_assignments (subject, role_id, created_at)
				 SELECT $1, id, now() FROM roles WHERE org_id = $2 AND slug = $3
				 ON CONFLICT DO NOTHING`,
				email, orgID, RoleAdmin,
			)
			if err != nil {
				return fmt.Errorf("assigning admin role: %w", err)
			}
			return nil
		}
	}

	// Check if user already has any assignments.
	var count int
	err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM role_assignments WHERE subject = $1`, email,
	).Scan(&count)
	if err != nil {
		return fmt.Errorf("checking existing assignments: %w", err)
	}

	// If no assignments, assign the default role.
	if count == 0 {
		_, err := pool.Exec(ctx,
			`INSERT INTO role_assignments (subject, role_id, created_at)
			 SELECT $1, id, now() FROM roles WHERE org_id = $2 AND slug = $3
			 ON CONFLICT DO NOTHING`,
			email, orgID, defaultRoleSlug,
		)
		if err != nil {
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
