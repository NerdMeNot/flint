package auth

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

// diffRoleAssignments drives the IdP group→role reconciliation: which role IDs
// to grant (mapped now, not yet held) and which to revoke (held from the IdP but
// no longer mapped). Manual grants never reach this function.
func TestDiffRoleAssignments(t *testing.T) {
	set := func(ids ...string) map[string]bool {
		m := make(map[string]bool, len(ids))
		for _, id := range ids {
			m[id] = true
		}
		return m
	}

	tests := []struct {
		name       string
		desired    map[string]bool
		current    []string
		wantGrant  []string
		wantRevoke []string
	}{
		{
			name:       "grant new, revoke stale",
			desired:    set("a"),
			current:    []string{"b"},
			wantGrant:  []string{"a"},
			wantRevoke: []string{"b"},
		},
		{
			name:    "no change when already aligned",
			desired: set("a", "b"),
			current: []string{"a", "b"},
		},
		{
			name:      "first login grants all mapped",
			desired:   set("a", "b"),
			current:   nil,
			wantGrant: []string{"a", "b"},
		},
		{
			name:       "left all groups revokes all idp grants",
			desired:    set(),
			current:    []string{"a", "b"},
			wantRevoke: []string{"a", "b"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grant, revoke := diffRoleAssignments(tt.desired, tt.current)
			sort.Strings(grant)
			sort.Strings(revoke)
			assert.Equal(t, tt.wantGrant, grant)
			assert.Equal(t, tt.wantRevoke, revoke)
		})
	}
}
