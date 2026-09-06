package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Joining a team must grant the roles assigned to that team, immediately.
//
// Grouping rules used to be written only by RegeneratePolicies at boot, so a
// user added to a privileged team gained nothing until the next restart.
func TestApplyGrouping_JoiningATeamGrantsItsRole(t *testing.T) {
	e, err := NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddPolicy(TeamPrincipal("eng"), "org-1", "*", "*", ObjRun, ActRead)
	require.NoError(t, err)

	allowed, err := e.Enforce("alice@x.dev", "org-1", "team-a", "prod", ObjRun, ActRead)
	require.NoError(t, err)
	require.False(t, allowed, "not a member yet")

	require.NoError(t, applyGroupingForUser(e, "alice@x.dev", []string{TeamPrincipal("eng")}))

	allowed, err = e.Enforce("alice@x.dev", "org-1", "team-a", "prod", ObjRun, ActRead)
	require.NoError(t, err)
	require.True(t, allowed, "joining the team must grant its role")
}

// Leaving a team must take the access away. This is the worse half of the bug:
// a stale grouping rule meant a removed member kept everything.
func TestApplyGrouping_LeavingATeamRevokesItsRole(t *testing.T) {
	e, err := NewMemoryEnforcer()
	require.NoError(t, err)
	_, err = e.AddPolicy(TeamPrincipal("eng"), "org-1", "*", "*", ObjRun, ActRead)
	require.NoError(t, err)
	require.NoError(t, applyGroupingForUser(e, "alice@x.dev", []string{TeamPrincipal("eng")}))

	// Now she leaves every team.
	require.NoError(t, applyGroupingForUser(e, "alice@x.dev", nil))

	allowed, err := e.Enforce("alice@x.dev", "org-1", "team-a", "prod", ObjRun, ActRead)
	require.NoError(t, err)
	require.False(t, allowed, "leaving the team must revoke its role")
}

// Reconciliation is a diff, not a rebuild: memberships that did not change are
// left alone, and unrelated users are untouched.
func TestApplyGrouping_IsADiff(t *testing.T) {
	e, err := NewMemoryEnforcer()
	require.NoError(t, err)
	require.NoError(t, applyGroupingForUser(e, "alice@x.dev",
		[]string{TeamPrincipal("eng"), TeamPrincipal("sre")}))
	require.NoError(t, applyGroupingForUser(e, "bob@x.dev", []string{TeamPrincipal("eng")}))

	// Alice swaps sre for security; eng stays.
	require.NoError(t, applyGroupingForUser(e, "alice@x.dev",
		[]string{TeamPrincipal("eng"), TeamPrincipal("security")}))

	alice, err := e.GetRolesForUser("alice@x.dev")
	require.NoError(t, err)
	require.ElementsMatch(t, []string{TeamPrincipal("eng"), TeamPrincipal("security")}, alice)

	bob, err := e.GetRolesForUser("bob@x.dev")
	require.NoError(t, err)
	require.Equal(t, []string{TeamPrincipal("eng")}, bob, "another user's membership must not be disturbed")
}

// Running it twice with the same input must not change anything the second
// time — it is called on every login and every SCIM push.
func TestApplyGrouping_IsIdempotent(t *testing.T) {
	e, err := NewMemoryEnforcer()
	require.NoError(t, err)
	desired := []string{TeamPrincipal("eng")}

	require.NoError(t, applyGroupingForUser(e, "alice@x.dev", desired))
	first, err := e.GetRolesForUser("alice@x.dev")
	require.NoError(t, err)

	require.NoError(t, applyGroupingForUser(e, "alice@x.dev", desired))
	second, err := e.GetRolesForUser("alice@x.dev")
	require.NoError(t, err)

	require.Equal(t, first, second)
}
