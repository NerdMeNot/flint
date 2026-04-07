package auth

import (
	"context"
	"fmt"

	"github.com/casbin/casbin/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ────────────────────────────────────────────────────────────
// Data types for policy generation
// ────────────────────────────────────────────────────────────

// RoleWithScope is a role definition loaded from the DB,
// including its permissions and scope.
type RoleWithScope struct {
	ID           string
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
func RegeneratePolicies(ctx context.Context, pool *pgxpool.Pool, enforcer *casbin.Enforcer) error {
	roles, err := loadAllRolesWithScope(ctx, pool)
	if err != nil {
		return fmt.Errorf("loading roles: %w", err)
	}
	roleMap := make(map[string]*RoleWithScope, len(roles))
	for i := range roles {
		roleMap[roles[i].ID] = &roles[i]
	}

	assignments, err := loadAllAssignments(ctx, pool)
	if err != nil {
		return fmt.Errorf("loading assignments: %w", err)
	}

	teamMembers, err := loadAllTeamMembers(ctx, pool)
	if err != nil {
		return fmt.Errorf("loading team members: %w", err)
	}

	apiKeys, err := loadAllAPIKeysWithScope(ctx, pool)
	if err != nil {
		return fmt.Errorf("loading api keys: %w", err)
	}

	// Clear all existing policies.
	enforcer.ClearPolicy()

	// Generate policies for each assignment.
	for _, a := range assignments {
		role, ok := roleMap[a.RoleID]
		if !ok {
			continue
		}
		addSubjectPolicies(enforcer, a.Subject, role)
	}

	// Generate team grouping rules.
	for teamSlug, members := range teamMembers {
		teamSubject := "team:" + teamSlug
		for _, userEmail := range members {
			enforcer.AddGroupingPolicy(userEmail, teamSubject)
		}
	}

	// Generate API key policies.
	for _, ak := range apiKeys {
		role, ok := roleMap[ak.RoleID]
		if !ok {
			continue
		}
		addAPIKeyPolicies(enforcer, ak, role)
	}

	// Persist to database.
	if err := enforcer.SavePolicy(); err != nil {
		return fmt.Errorf("saving policies: %w", err)
	}

	return nil
}

// RegenerateForSubject rebuilds Casbin policies for a single subject.
// Called when an assignment is created or deleted.
func RegenerateForSubject(ctx context.Context, pool *pgxpool.Pool, enforcer *casbin.Enforcer, subject string) error {
	// Remove existing policies for this subject.
	enforcer.RemoveFilteredPolicy(0, subject)

	// Load this subject's assignments.
	assignments, err := loadAssignmentsForSubject(ctx, pool, subject)
	if err != nil {
		return fmt.Errorf("loading assignments for %s: %w", subject, err)
	}

	for _, a := range assignments {
		role, err := loadRoleWithScope(ctx, pool, a.RoleID)
		if err != nil {
			return fmt.Errorf("loading role %s: %w", a.RoleID, err)
		}
		addSubjectPolicies(enforcer, subject, role)
	}

	return enforcer.SavePolicy()
}

// RegenerateForRole rebuilds Casbin policies for all subjects
// assigned to a specific role. Called when a role is updated.
func RegenerateForRole(ctx context.Context, pool *pgxpool.Pool, enforcer *casbin.Enforcer, roleID string) error {
	assignments, err := loadAssignmentsForRole(ctx, pool, roleID)
	if err != nil {
		return fmt.Errorf("loading assignments for role %s: %w", roleID, err)
	}

	for _, a := range assignments {
		if err := RegenerateForSubject(ctx, pool, enforcer, a.Subject); err != nil {
			return err
		}
	}

	return nil
}

// ────────────────────────────────────────────────────────────
// Policy generation logic
// ────────────────────────────────────────────────────────────

// addSubjectPolicies generates and adds Casbin p rules for a subject's role.
func addSubjectPolicies(enforcer *casbin.Enforcer, subject string, role *RoleWithScope) {
	expanded := ExpandImplications(role.Permissions)

	if IsWildcard(role.Permissions) {
		// Wildcard role: full access everywhere.
		enforcer.AddPolicy(subject, "*", "*", "*", "*")
		return
	}

	for _, perm := range expanded {
		if IsAdminObject(perm.Object) {
			// Admin permissions: always platform-wide.
			enforcer.AddPolicy(subject, "*", "*", perm.Object, perm.Action)
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
					enforcer.AddPolicy(subject, ws, env, perm.Object, perm.Action)
				}
			}
		}
	}
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
func addAPIKeyPolicies(enforcer *casbin.Enforcer, key APIKeyWithScope, role *RoleWithScope) {
	subject := "apikey:" + key.ID
	expanded := ExpandImplications(role.Permissions)

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
				enforcer.AddPolicy(subject, w, e, "*", "*")
			}
		}
		return
	}

	for _, perm := range expanded {
		if IsAdminObject(perm.Object) {
			enforcer.AddPolicy(subject, "*", "*", perm.Object, perm.Action)
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
					enforcer.AddPolicy(subject, w, e, perm.Object, perm.Action)
				}
			}
		}
	}
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
// Database loaders (raw pgx queries — no sqlc dependency)
// ────────────────────────────────────────────────────────────

func loadAllRolesWithScope(ctx context.Context, pool *pgxpool.Pool) ([]RoleWithScope, error) {
	rows, err := pool.Query(ctx, `
		SELECT r.id, r.slug, r.is_system
		FROM roles r
		ORDER BY r.slug`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []RoleWithScope
	for rows.Next() {
		var r RoleWithScope
		if err := rows.Scan(&r.ID, &r.Slug, &r.IsSystem); err != nil {
			return nil, err
		}
		roles = append(roles, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for i := range roles {
		roles[i].Permissions, err = loadPermissionsForRole(ctx, pool, roles[i].ID)
		if err != nil {
			return nil, err
		}
		roles[i].Workspaces, err = loadWorkspaceScopeForRole(ctx, pool, roles[i].ID)
		if err != nil {
			return nil, err
		}
		roles[i].Environments, err = loadEnvironmentScopeForRole(ctx, pool, roles[i].ID)
		if err != nil {
			return nil, err
		}
	}

	return roles, nil
}

func loadRoleWithScope(ctx context.Context, pool *pgxpool.Pool, roleID string) (*RoleWithScope, error) {
	var r RoleWithScope
	err := pool.QueryRow(ctx,
		`SELECT id, slug, is_system FROM roles WHERE id = $1`, roleID,
	).Scan(&r.ID, &r.Slug, &r.IsSystem)
	if err != nil {
		return nil, err
	}

	r.Permissions, err = loadPermissionsForRole(ctx, pool, roleID)
	if err != nil {
		return nil, err
	}
	r.Workspaces, err = loadWorkspaceScopeForRole(ctx, pool, roleID)
	if err != nil {
		return nil, err
	}
	r.Environments, err = loadEnvironmentScopeForRole(ctx, pool, roleID)
	if err != nil {
		return nil, err
	}

	return &r, nil
}

func loadPermissionsForRole(ctx context.Context, pool *pgxpool.Pool, roleID string) ([]Permission, error) {
	rows, err := pool.Query(ctx,
		`SELECT object, action FROM role_permissions WHERE role_id = $1`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var perms []Permission
	for rows.Next() {
		var p Permission
		if err := rows.Scan(&p.Object, &p.Action); err != nil {
			return nil, err
		}
		perms = append(perms, p)
	}
	return perms, rows.Err()
}

func loadWorkspaceScopeForRole(ctx context.Context, pool *pgxpool.Pool, roleID string) ([]string, error) {
	rows, err := pool.Query(ctx,
		`SELECT w.slug FROM role_workspace_scope rws
		 JOIN workspaces w ON w.id = rws.workspace_id
		 WHERE rws.role_id = $1`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var slugs []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		slugs = append(slugs, s)
	}
	return slugs, rows.Err()
}

func loadEnvironmentScopeForRole(ctx context.Context, pool *pgxpool.Pool, roleID string) ([]string, error) {
	rows, err := pool.Query(ctx,
		`SELECT pe.name FROM role_environment_scope res
		 JOIN protected_environments pe ON pe.id = res.environment_id
		 WHERE res.role_id = $1`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		names = append(names, s)
	}
	return names, rows.Err()
}

func loadAllAssignments(ctx context.Context, pool *pgxpool.Pool) ([]Assignment, error) {
	rows, err := pool.Query(ctx,
		`SELECT subject, role_id FROM role_assignments`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Assignment
	for rows.Next() {
		var a Assignment
		if err := rows.Scan(&a.Subject, &a.RoleID); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func loadAssignmentsForSubject(ctx context.Context, pool *pgxpool.Pool, subject string) ([]Assignment, error) {
	rows, err := pool.Query(ctx,
		`SELECT subject, role_id FROM role_assignments WHERE subject = $1`, subject)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Assignment
	for rows.Next() {
		var a Assignment
		if err := rows.Scan(&a.Subject, &a.RoleID); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func loadAssignmentsForRole(ctx context.Context, pool *pgxpool.Pool, roleID string) ([]Assignment, error) {
	rows, err := pool.Query(ctx,
		`SELECT subject, role_id FROM role_assignments WHERE role_id = $1`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []Assignment
	for rows.Next() {
		var a Assignment
		if err := rows.Scan(&a.Subject, &a.RoleID); err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

func loadAllTeamMembers(ctx context.Context, pool *pgxpool.Pool) (map[string][]string, error) {
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

func loadAllAPIKeysWithScope(ctx context.Context, pool *pgxpool.Pool) ([]APIKeyWithScope, error) {
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
		keys[i].Workspaces, err = loadAPIKeyWorkspaceScope(ctx, pool, keys[i].ID)
		if err != nil {
			return nil, err
		}
		keys[i].Environments, err = loadAPIKeyEnvironmentScope(ctx, pool, keys[i].ID)
		if err != nil {
			return nil, err
		}
	}

	return keys, nil
}

func loadAPIKeyWorkspaceScope(ctx context.Context, pool *pgxpool.Pool, keyID string) ([]string, error) {
	rows, err := pool.Query(ctx,
		`SELECT w.slug FROM api_key_workspace_scope akws
		 JOIN workspaces w ON w.id = akws.workspace_id
		 WHERE akws.api_key_id = $1`, keyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var slugs []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		slugs = append(slugs, s)
	}
	return slugs, rows.Err()
}

func loadAPIKeyEnvironmentScope(ctx context.Context, pool *pgxpool.Pool, keyID string) ([]string, error) {
	rows, err := pool.Query(ctx,
		`SELECT pe.name FROM api_key_environment_scope akes
		 JOIN protected_environments pe ON pe.id = akes.environment_id
		 WHERE akes.api_key_id = $1`, keyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		names = append(names, s)
	}
	return names, rows.Err()
}
