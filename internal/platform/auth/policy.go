package auth

import (
	"context"
	"fmt"

	"github.com/casbin/casbin/v2"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// ────────────────────────────────────────────────────────────
// Data types for policy generation
// ────────────────────────────────────────────────────────────

// RoleWithScope is a role definition loaded from the DB,
// including its permissions and scope.
type RoleWithScope struct {
	ID           string
	OrgID        string
	Slug         string
	IsSystem     bool
	Permissions  []Permission
	Workspaces   []string // slugs; empty = all
	Environments []string // names; empty = all
}

// Assignment is a subject → role mapping from the DB.
type Assignment struct {
	Subject string
	RoleID  string
}

// ────────────────────────────────────────────────────────────
// Policy regeneration
// ────────────────────────────────────────────────────────────

// RegeneratePolicies performs a full rebuild of all Casbin policies
// from the database. Called at boot and after migrations.
func RegeneratePolicies(ctx context.Context, q db.Querier, pool db.Pool, enforcer casbin.IEnforcer) error {
	roles, err := loadAllRolesWithScope(ctx, q, pool)
	if err != nil {
		return fmt.Errorf("loading roles: %w", err)
	}
	roleMap := make(map[string]*RoleWithScope, len(roles))
	for i := range roles {
		roleMap[roles[i].ID] = &roles[i]
	}

	assignments, err := loadAllAssignments(ctx, q)
	if err != nil {
		return fmt.Errorf("loading assignments: %w", err)
	}

	teamMembers, err := loadAllTeamMembers(ctx, pool)
	if err != nil {
		return fmt.Errorf("loading team members: %w", err)
	}

	apiKeys, err := loadAllAPIKeysWithScope(ctx, q, pool)
	if err != nil {
		return fmt.Errorf("loading api keys: %w", err)
	}

	// Clear all existing policies. This is the one place a full rewrite is
	// correct: it runs at boot, from the same DB tables every replica derives
	// from, so the result is deterministic rather than one replica's private
	// view. SavePolicy below is what makes the DELETE durable.
	enforcer.ClearPolicy()

	// Generate policies for each assignment.
	for _, a := range assignments {
		role, ok := roleMap[a.RoleID]
		if !ok {
			continue
		}
		if err := addSubjectPolicies(enforcer, a.Subject, role); err != nil {
			return fmt.Errorf("adding policies for %s: %w", a.Subject, err)
		}
	}

	// Generate team grouping rules.
	for teamSlug, members := range teamMembers {
		teamSubject := "team:" + teamSlug
		for _, userEmail := range members {
			if _, err := enforcer.AddGroupingPolicy(userEmail, teamSubject); err != nil {
				return fmt.Errorf("adding %s to %s: %w", userEmail, teamSubject, err)
			}
		}
	}

	// Generate API key policies.
	for _, ak := range apiKeys {
		role, ok := roleMap[ak.RoleID]
		if !ok {
			continue
		}
		if err := addAPIKeyPolicies(enforcer, ak, role); err != nil {
			return fmt.Errorf("adding policies for api key %s: %w", ak.ID, err)
		}
	}

	// Persist to database.
	if err := enforcer.SavePolicy(); err != nil {
		return fmt.Errorf("saving policies: %w", err)
	}

	return nil
}

// RegenerateGroupingForUser makes the Casbin grouping rules for one user match
// their current team membership.
//
// Grouping rules used to be written only by RegeneratePolicies at boot, so
// every runtime membership change — the teams API, SCIM group updates, IdP
// group sync — left them stale. Roles can be assigned to a "team:<slug>"
// subject, so that meant a user added to a privileged team gained nothing until
// the next restart and, worse, a user removed from one kept everything.
func RegenerateGroupingForUser(ctx context.Context, pool db.Pool, enforcer casbin.IEnforcer, userEmail string) error {
	if userEmail == "" {
		return nil
	}

	desired, err := loadTeamSubjectsForUser(ctx, pool, userEmail)
	if err != nil {
		return fmt.Errorf("loading teams for %s: %w", userEmail, err)
	}
	return applyGroupingForUser(enforcer, userEmail, desired)
}

// applyGroupingForUser reconciles a user's grouping rules against the team
// subjects they should now hold. Split out from the DB read so the diff — the
// part that can actually be wrong — is testable without a live Postgres.
func applyGroupingForUser(enforcer casbin.IEnforcer, userEmail string, desired []string) error {
	current, err := enforcer.GetRolesForUser(userEmail)
	if err != nil {
		return fmt.Errorf("listing current groups for %s: %w", userEmail, err)
	}

	desiredSet := make(map[string]bool, len(desired))
	for _, t := range desired {
		desiredSet[t] = true
	}
	currentSet := make(map[string]bool, len(current))
	for _, t := range current {
		currentSet[t] = true
	}

	for _, t := range desired {
		if currentSet[t] {
			continue
		}
		if _, err := enforcer.AddGroupingPolicy(userEmail, t); err != nil {
			return fmt.Errorf("adding %s to %s: %w", userEmail, t, err)
		}
	}
	// A dropped removal is a permission the user silently keeps, so this error
	// is not swallowed either.
	for _, t := range current {
		if desiredSet[t] {
			continue
		}
		if _, err := enforcer.RemoveGroupingPolicy(userEmail, t); err != nil {
			return fmt.Errorf("removing %s from %s: %w", userEmail, t, err)
		}
	}

	return nil
}

func loadTeamSubjectsForUser(ctx context.Context, pool db.Pool, userEmail string) ([]string, error) {
	rows, err := pool.Query(ctx,
		`SELECT 'team:' || t.slug
		 FROM team_members tm
		 JOIN teams t ON t.id = tm.team_id
		 JOIN users u ON u.id = tm.user_id
		 WHERE lower(u.email) = lower($1)`, userEmail)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var teams []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		teams = append(teams, s)
	}
	return teams, rows.Err()
}

// RegenerateForSubject rebuilds Casbin policies for a single subject.
// Called when an assignment is created or deleted.
func RegenerateForSubject(ctx context.Context, q db.Querier, pool db.Pool, enforcer casbin.IEnforcer, subject string) error {
	// Remove existing policies for this subject. A failure here must abort:
	// continuing would ADD the new rules on top of the stale ones the removal
	// was meant to clear, leaving the subject with revoked permissions intact.
	if _, err := enforcer.RemoveFilteredPolicy(0, subject); err != nil {
		return fmt.Errorf("clearing policies for %s: %w", subject, err)
	}

	// Load this subject's assignments.
	assignments, err := loadAssignmentsForSubject(ctx, q, subject)
	if err != nil {
		return fmt.Errorf("loading assignments for %s: %w", subject, err)
	}

	for _, a := range assignments {
		role, err := loadRoleWithScope(ctx, q, pool, a.RoleID)
		if err != nil {
			return fmt.Errorf("loading role %s: %w", a.RoleID, err)
		}
		if err := addSubjectPolicies(enforcer, subject, role); err != nil {
			return fmt.Errorf("adding policies for %s: %w", subject, err)
		}
	}

	// No SavePolicy here. Autosave has already written each rule through
	// individually; SavePolicy would additionally DELETE every row in
	// casbin_rules and reinsert this replica's in-memory snapshot, discarding
	// anything a peer wrote since this process booted. Since this runs on every
	// SSO login, that turned a routine login on one replica into silent
	// revocation for users granted access on another.
	return nil
}

// RegenerateForRole rebuilds Casbin policies for all subjects
// assigned to a specific role. Called when a role is updated.
func RegenerateForRole(ctx context.Context, q db.Querier, pool db.Pool, enforcer casbin.IEnforcer, roleID string) error {
	assignments, err := loadAssignmentsForRole(ctx, q, roleID)
	if err != nil {
		return fmt.Errorf("loading assignments for role %s: %w", roleID, err)
	}

	for _, a := range assignments {
		if err := RegenerateForSubject(ctx, q, pool, enforcer, a.Subject); err != nil {
			return err
		}
	}

	return nil
}

// ────────────────────────────────────────────────────────────
// Policy generation logic
// ────────────────────────────────────────────────────────────

// addSubjectPolicies generates and adds Casbin p rules for a subject's role.
//
// Every AddPolicy is checked. A dropped rule here is a permission the user
// silently does not have (or, on the removal path, one they silently keep) —
// an authorization outcome decided by an ignored error, which is exactly the
// kind of failure that must be loud.
func addSubjectPolicies(enforcer casbin.IEnforcer, subject string, role *RoleWithScope) error {
	expanded := ExpandImplications(role.Permissions)
	org, err := policyOrg(role.OrgID)
	if err != nil {
		return fmt.Errorf("subject %s, role %s: %w", subject, role.Slug, err)
	}

	if IsWildcard(role.Permissions) {
		// Wildcard role: full access everywhere in its org.
		_, err := enforcer.AddPolicy(subject, org, "*", "*", "*", "*")
		return err
	}

	for _, perm := range expanded {
		if IsAdminObject(perm.Object) {
			// Admin permissions: platform-wide within the org. The resources
			// behind them (runners, connections, secrets, roles) carry no
			// workspace column, so there is nothing to scope against — see
			// ValidateRoleScope, which refuses to create a role that implies
			// otherwise.
			if _, err := enforcer.AddPolicy(subject, org, "*", "*", perm.Object, perm.Action); err != nil {
				return err
			}
		} else if IsCIObject(perm.Object) {
			// CI permissions: scope-aware.
			workspaces := role.Workspaces
			if len(workspaces) == 0 {
				workspaces = []string{"*"}
			}
			environments := role.Environments
			if len(environments) == 0 {
				environments = []string{"*"}
			}

			for _, ws := range workspaces {
				for _, env := range environments {
					if _, err := enforcer.AddPolicy(subject, org, ws, env, perm.Object, perm.Action); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// policyOrg maps a role's org to the policy's org field.
//
// An empty org id is a programming error — roles.org_id is NOT NULL — so it is
// reported rather than substituted. This used to return "*", which turned a
// malformed grant into a grant valid in *every* org: the one place in the authz
// path that failed open, and the failure would have been invisible precisely
// because the resulting policy matches more than intended, not less.
func policyOrg(orgID string) (string, error) {
	if orgID == "" {
		return "", fmt.Errorf("role has no org id: refusing to generate a policy that would match every org")
	}
	return orgID, nil
}

// APIKeyWithScope holds an API key's role and optional scope restriction.
type APIKeyWithScope struct {
	ID           string
	RoleID       string
	Workspaces   []string // restriction; empty = inherit role scope
	Environments []string // restriction; empty = inherit role scope
}

// addAPIKeyPolicies generates Casbin p rules for an API key.
// The effective scope is the intersection of the role's scope and the key's restriction.
func addAPIKeyPolicies(enforcer casbin.IEnforcer, key APIKeyWithScope, role *RoleWithScope) error {
	subject := APIKeyPrincipal(key.ID)
	expanded := ExpandImplications(role.Permissions)
	org, err := policyOrg(role.OrgID)
	if err != nil {
		return fmt.Errorf("api key %s, role %s: %w", key.ID, role.Slug, err)
	}

	if IsWildcard(role.Permissions) {
		// Even wildcard roles can be restricted by the key.
		ws := key.Workspaces
		env := key.Environments
		if len(ws) == 0 {
			ws = []string{"*"}
		}
		if len(env) == 0 {
			env = []string{"*"}
		}
		for _, w := range ws {
			for _, e := range env {
				if _, err := enforcer.AddPolicy(subject, org, w, e, "*", "*"); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for _, perm := range expanded {
		if IsAdminObject(perm.Object) {
			if _, err := enforcer.AddPolicy(subject, org, "*", "*", perm.Object, perm.Action); err != nil {
				return err
			}
		} else if IsCIObject(perm.Object) {
			// Effective scope = intersection of role scope and key restriction.
			ws := intersectScope(role.Workspaces, key.Workspaces)
			env := intersectScope(role.Environments, key.Environments)

			if len(ws) == 0 {
				ws = []string{"*"}
			}
			if len(env) == 0 {
				env = []string{"*"}
			}

			for _, w := range ws {
				for _, e := range env {
					if _, err := enforcer.AddPolicy(subject, org, w, e, perm.Object, perm.Action); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

// intersectScope computes the intersection of role scope and key restriction.
// Empty role scope = all (represented as nil/empty). Empty key restriction = inherit role scope.
// If both are non-empty, the result is the intersection.
func intersectScope(roleScope, keyRestriction []string) []string {
	if len(keyRestriction) == 0 {
		return roleScope // key inherits role scope
	}
	if len(roleScope) == 0 {
		return keyRestriction // role is unscoped, key narrows
	}

	// Both specified: intersection.
	roleSet := make(map[string]bool, len(roleScope))
	for _, s := range roleScope {
		roleSet[s] = true
	}
	var result []string
	for _, s := range keyRestriction {
		if roleSet[s] {
			result = append(result, s)
		}
	}
	return result
}

// ────────────────────────────────────────────────────────────
// Database loaders (sqlc type-safe queries + raw pgx for non-scope tables)
// ────────────────────────────────────────────────────────────

func loadAllRolesWithScope(ctx context.Context, q db.Querier, pool db.Pool) ([]RoleWithScope, error) {
	// The roles table itself is not covered by scope queries; use raw pgx.
	rows, err := pool.Query(ctx, `
		SELECT r.id, r.org_id, r.slug, r.is_system
		FROM roles r
		ORDER BY r.slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []RoleWithScope
	for rows.Next() {
		var r RoleWithScope
		if err := rows.Scan(&r.ID, &r.OrgID, &r.Slug, &r.IsSystem); err != nil {
			return nil, err
		}
		roles = append(roles, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range roles {
		roles[i].Permissions, err = loadPermissionsForRole(ctx, q, roles[i].ID)
		if err != nil {
			return nil, err
		}
		roles[i].Workspaces, err = loadWorkspaceScopeForRole(ctx, q, roles[i].ID)
		if err != nil {
			return nil, err
		}
		roles[i].Environments, err = loadEnvironmentScopeForRole(ctx, q, roles[i].ID)
		if err != nil {
			return nil, err
		}
	}

	return roles, nil
}

func loadRoleWithScope(ctx context.Context, q db.Querier, pool db.Pool, roleID string) (*RoleWithScope, error) {
	// The roles table itself is not covered by scope queries; use raw pgx.
	var r RoleWithScope
	err := pool.QueryRow(ctx,
		`SELECT id, org_id, slug, is_system FROM roles WHERE id = $1`, roleID,
	).Scan(&r.ID, &r.OrgID, &r.Slug, &r.IsSystem)
	if err != nil {
		return nil, err
	}

	r.Permissions, err = loadPermissionsForRole(ctx, q, roleID)
	if err != nil {
		return nil, err
	}
	r.Workspaces, err = loadWorkspaceScopeForRole(ctx, q, roleID)
	if err != nil {
		return nil, err
	}
	r.Environments, err = loadEnvironmentScopeForRole(ctx, q, roleID)
	if err != nil {
		return nil, err
	}

	return &r, nil
}

func loadPermissionsForRole(ctx context.Context, q db.Querier, roleID string) ([]Permission, error) {
	rows, err := q.ListRolePermissions(ctx, roleID)
	if err != nil {
		return nil, err
	}
	perms := make([]Permission, len(rows))
	for i, r := range rows {
		perms[i] = Permission{Object: r.Object, Action: r.Action}
	}
	return perms, nil
}

func loadWorkspaceScopeForRole(ctx context.Context, q db.Querier, roleID string) ([]string, error) {
	return q.ListRoleWorkspaceSlugs(ctx, roleID)
}

func loadEnvironmentScopeForRole(ctx context.Context, q db.Querier, roleID string) ([]string, error) {
	return q.ListRoleEnvironmentNames(ctx, roleID)
}

func loadAllAssignments(ctx context.Context, q db.Querier) ([]Assignment, error) {
	rows, err := q.ListAllRoleAssignments(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]Assignment, len(rows))
	for i, r := range rows {
		result[i] = Assignment{Subject: r.Subject, RoleID: r.RoleID}
	}
	return result, nil
}

func loadAssignmentsForSubject(ctx context.Context, q db.Querier, subject string) ([]Assignment, error) {
	rows, err := q.ListRoleAssignmentsBySubject(ctx, subject)
	if err != nil {
		return nil, err
	}
	result := make([]Assignment, len(rows))
	for i, r := range rows {
		result[i] = Assignment{Subject: r.Subject, RoleID: r.RoleID}
	}
	return result, nil
}

func loadAssignmentsForRole(ctx context.Context, q db.Querier, roleID string) ([]Assignment, error) {
	rows, err := q.ListRoleAssignmentsByRole(ctx, roleID)
	if err != nil {
		return nil, err
	}
	result := make([]Assignment, len(rows))
	for i, r := range rows {
		result[i] = Assignment{Subject: r.Subject, RoleID: r.RoleID}
	}
	return result, nil
}

func loadAllTeamMembers(ctx context.Context, pool db.Pool) (map[string][]string, error) {
	rows, err := pool.Query(ctx,
		`SELECT t.slug, u.email
		 FROM team_members tm
		 JOIN teams t ON t.id = tm.team_id
		 JOIN users u ON u.id = tm.user_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string][]string)
	for rows.Next() {
		var slug, email string
		if err := rows.Scan(&slug, &email); err != nil {
			return nil, err
		}
		result[slug] = append(result[slug], email)
	}
	return result, rows.Err()
}

func loadAllAPIKeysWithScope(ctx context.Context, q db.Querier, pool db.Pool) ([]APIKeyWithScope, error) {
	// The api_keys table itself is not covered by scope queries; use raw pgx.
	rows, err := pool.Query(ctx,
		`SELECT id, role_id FROM api_keys WHERE role_id IS NOT NULL AND (expires_at IS NULL OR expires_at > now())`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []APIKeyWithScope
	for rows.Next() {
		var k APIKeyWithScope
		if err := rows.Scan(&k.ID, &k.RoleID); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range keys {
		keys[i].Workspaces, err = loadAPIKeyWorkspaceScope(ctx, q, keys[i].ID)
		if err != nil {
			return nil, err
		}
		keys[i].Environments, err = loadAPIKeyEnvironmentScope(ctx, q, keys[i].ID)
		if err != nil {
			return nil, err
		}
	}

	return keys, nil
}

func loadAPIKeyWorkspaceScope(ctx context.Context, q db.Querier, keyID string) ([]string, error) {
	return q.ListAPIKeyWorkspaceSlugs(ctx, keyID)
}

func loadAPIKeyEnvironmentScope(ctx context.Context, q db.Querier, keyID string) ([]string, error) {
	return q.ListAPIKeyEnvironmentSlugs(ctx, keyID)
}
