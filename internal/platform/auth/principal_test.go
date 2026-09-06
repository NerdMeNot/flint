package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPermittedWorkspaces_ScopedSubject(t *testing.T) {
	e, err := NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddPolicy("alice@x.dev", "org-1", "team-a", "*", ObjProject, ActRead)
	require.NoError(t, err)
	_, err = e.AddPolicy("alice@x.dev", "org-1", "team-b", "*", ObjProject, ActRead)
	require.NoError(t, err)

	slugs, all, err := PermittedWorkspaces(e, "alice@x.dev", "org-1", ObjProject, ActRead)
	require.NoError(t, err)
	require.False(t, all)
	require.Equal(t, []string{"team-a", "team-b"}, slugs)
}

func TestPermittedWorkspaces_WildcardMeansAll(t *testing.T) {
	e, err := NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddPolicy("root@x.dev", "org-1", "*", "*", "*", "*")
	require.NoError(t, err)

	slugs, all, err := PermittedWorkspaces(e, "root@x.dev", "org-1", ObjProject, ActRead)
	require.NoError(t, err)
	require.True(t, all)
	require.Empty(t, slugs)
}

func TestPermittedWorkspaces_IgnoresOtherOrgs(t *testing.T) {
	e, err := NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddPolicy("alice@x.dev", "org-other", "team-a", "*", ObjProject, ActRead)
	require.NoError(t, err)

	slugs, all, err := PermittedWorkspaces(e, "alice@x.dev", "org-1", ObjProject, ActRead)
	require.NoError(t, err)
	require.False(t, all)
	require.Empty(t, slugs)
}

// Grants reached through a team must count: that is the whole point of the
// grouping rules.
func TestPermittedWorkspaces_FollowsTeamMembership(t *testing.T) {
	e, err := NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddGroupingPolicy("alice@x.dev", TeamPrincipal("eng"))
	require.NoError(t, err)
	_, err = e.AddPolicy(TeamPrincipal("eng"), "org-1", "team-a", "*", ObjProject, ActRead)
	require.NoError(t, err)

	slugs, all, err := PermittedWorkspaces(e, "alice@x.dev", "org-1", ObjProject, ActRead)
	require.NoError(t, err)
	require.False(t, all)
	require.Equal(t, []string{"team-a"}, slugs)
}

func TestValidateRoleScope(t *testing.T) {
	ciOnly := []Permission{{Object: ObjProject, Action: ActRead}}
	withAdmin := []Permission{
		{Object: ObjProject, Action: ActRead},
		{Object: ObjSecret, Action: ActManage},
	}

	t.Run("unscoped role is always fine", func(t *testing.T) {
		require.NoError(t, ValidateRoleScope(withAdmin, nil, nil))
	})

	t.Run("scoped CI-only role is fine", func(t *testing.T) {
		require.NoError(t, ValidateRoleScope(ciOnly, []string{"team-a"}, nil))
	})

	t.Run("scoped role with admin permissions is refused", func(t *testing.T) {
		err := ValidateRoleScope(withAdmin, []string{"team-a"}, nil)
		require.Error(t, err)
		require.Contains(t, err.Error(), "secret")
	})

	t.Run("environment scope is checked too", func(t *testing.T) {
		require.Error(t, ValidateRoleScope(withAdmin, nil, []string{"production"}))
	})

	t.Run("wildcard role cannot be scoped", func(t *testing.T) {
		wildcard := []Permission{{Object: ObjWildcard, Action: ActWildcard}}
		require.Error(t, ValidateRoleScope(wildcard, []string{"team-a"}, nil))
	})
}

// A role with no org id is a programming error (roles.org_id is NOT NULL), and
// the old behaviour substituted "*" — turning a malformed grant into one valid
// in every org. Failing open is the worst direction for this to fail in,
// because the resulting policy matches more than intended, not less.
func TestAddSubjectPolicies_RefusesAGrantWithNoOrg(t *testing.T) {
	e, err := NewMemoryEnforcer()
	require.NoError(t, err)

	role := &RoleWithScope{
		ID: "r1", Slug: "broken", OrgID: "",
		Permissions: []Permission{{Object: ObjProject, Action: ActRead}},
	}

	err = addSubjectPolicies(e, "alice@x.dev", role)
	require.Error(t, err)
	require.Contains(t, err.Error(), "every org")

	policies, err := e.GetPolicy()
	require.NoError(t, err)
	require.Empty(t, policies, "no policy may be written from a malformed role")
}

func TestAddAPIKeyPolicies_RefusesAGrantWithNoOrg(t *testing.T) {
	e, err := NewMemoryEnforcer()
	require.NoError(t, err)

	role := &RoleWithScope{
		ID: "r1", Slug: "broken", OrgID: "",
		Permissions: []Permission{{Object: ObjProject, Action: ActRead}},
	}

	err = addAPIKeyPolicies(e, APIKeyWithScope{ID: "key-1", RoleID: "r1"}, role)
	require.Error(t, err)

	policies, err := e.GetPolicy()
	require.NoError(t, err)
	require.Empty(t, policies)
}

// A well-formed role still generates policies scoped to its own org.
func TestAddSubjectPolicies_ScopesToTheRoleOrg(t *testing.T) {
	e, err := NewMemoryEnforcer()
	require.NoError(t, err)

	role := &RoleWithScope{
		ID: "r1", Slug: "reader", OrgID: "org-1",
		Permissions: []Permission{{Object: ObjProject, Action: ActRead}},
	}
	require.NoError(t, addSubjectPolicies(e, "alice@x.dev", role))

	allowed, err := e.Enforce("alice@x.dev", "org-1", "any-ws", "any-env", ObjProject, ActRead)
	require.NoError(t, err)
	require.True(t, allowed)

	allowed, err = e.Enforce("alice@x.dev", "org-2", "any-ws", "any-env", ObjProject, ActRead)
	require.NoError(t, err)
	require.False(t, allowed, "the grant must not reach another org")
}
