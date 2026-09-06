package auth

import (
	"fmt"
	"sort"
	"strings"

	"github.com/casbin/casbin/v3"
)

// Principal prefixes. Policies are keyed on these, so anything that
// authenticates a request has to produce the matching form.
const (
	apiKeyPrefix = "apikey:"
	teamPrefix   = "team:"
)

// APIKeyPrincipal returns the RBAC subject for an API key.
func APIKeyPrincipal(keyID string) string { return apiKeyPrefix + keyID }

// TeamPrincipal returns the RBAC subject for a team.
func TeamPrincipal(slug string) string { return teamPrefix + slug }

// Policy field indices for p = sub, org, ws, env, obj, act.
const (
	pSub = iota
	pOrg
	pWorkspace
	pEnvironment
	pObject
	pAction
	policyFieldCount
)

// PermittedWorkspaces reports the workspaces in which a subject holds
// (obj, act), following team membership. `all` is true when the subject holds
// it platform-wide, in which case slugs is nil and no filtering is needed.
//
// This is what makes list routes work for scope-restricted users: the route
// authorizes with ScopeAny ("do they hold this anywhere?") and then narrows the
// result set to these workspaces. Without it the only way to express a scoped
// list was for the caller to pass ?workspace= — a value the caller chooses,
// which is not a basis for an access decision.
func PermittedWorkspaces(enforcer casbin.IEnforcer, subject, orgID, obj, act string) (slugs []string, all bool, err error) {
	if enforcer == nil {
		return nil, false, fmt.Errorf("no enforcer configured")
	}
	if subject == "" {
		return nil, false, fmt.Errorf("empty subject")
	}

	policies, err := enforcer.GetImplicitPermissionsForUser(subject)
	if err != nil {
		return nil, false, fmt.Errorf("listing permissions for %s: %w", subject, err)
	}

	set := make(map[string]bool)
	for _, p := range policies {
		if len(p) < policyFieldCount {
			continue
		}
		if p[pOrg] != "*" && p[pOrg] != orgID {
			continue
		}
		if p[pObject] != "*" && p[pObject] != obj {
			continue
		}
		if p[pAction] != "*" && p[pAction] != act {
			continue
		}
		if p[pWorkspace] == "*" {
			return nil, true, nil
		}
		set[p[pWorkspace]] = true
	}

	slugs = make([]string, 0, len(set))
	for s := range set {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs) // deterministic, so query plans and tests are stable
	return slugs, false, nil
}

// ValidateRoleScope rejects role definitions whose scope promises something the
// enforcement model cannot deliver.
//
// Admin permissions (secret, runner, connection, role, …) are emitted
// platform-wide because the resources behind them have no workspace column —
// there is nothing to scope them against. Combining them with a workspace or
// environment restriction therefore produces a role that reads as "admin of
// team-a" in the UI and behaves as "admin of everything". Rather than enforce
// a scope that silently does not exist, refuse to define one.
func ValidateRoleScope(perms []Permission, workspaces, environments []string) error {
	if len(workspaces) == 0 && len(environments) == 0 {
		return nil
	}

	var offenders []string
	seen := make(map[string]bool)
	for _, p := range ExpandImplications(perms) {
		if !IsAdminObject(p.Object) || seen[p.Object] {
			continue
		}
		seen[p.Object] = true
		offenders = append(offenders, p.Object)
	}
	if IsWildcard(perms) {
		return fmt.Errorf("a role granting *:* cannot be restricted to specific workspaces or environments: " +
			"remove the scope, or grant individual permissions instead")
	}
	if len(offenders) == 0 {
		return nil
	}
	sort.Strings(offenders)

	dimension := "workspaces"
	if len(workspaces) == 0 {
		dimension = "environments"
	}
	return fmt.Errorf("cannot restrict this role to specific %s: %s %s administered platform-wide, "+
		"so the restriction would not be enforced. Split the admin permissions into a separate unscoped role",
		dimension, strings.Join(offenders, ", "), plural(len(offenders)))
}

func plural(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}
