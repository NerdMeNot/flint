package auth

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsAdminObject(t *testing.T) {
	adminObjs := []string{ObjWorkspace, ObjTeam, ObjEnvironment, ObjRunner, ObjConnection, ObjAPIKey, ObjSecret, ObjRole, ObjAudit}
	for _, obj := range adminObjs {
		assert.True(t, IsAdminObject(obj), "expected %s to be admin", obj)
		assert.False(t, IsCIObject(obj), "expected %s to NOT be CI", obj)
	}
}

func TestIsCIObject(t *testing.T) {
	ciObjs := []string{ObjProject, ObjRun, ObjGate}
	for _, obj := range ciObjs {
		assert.True(t, IsCIObject(obj), "expected %s to be CI", obj)
		assert.False(t, IsAdminObject(obj), "expected %s to NOT be admin", obj)
	}
}

func TestExpandImplications_GateApprove(t *testing.T) {
	explicit := []Permission{{ObjGate, ActApprove}}
	expanded := ExpandImplications(explicit)

	keys := permKeys(expanded)
	assert.Contains(t, keys, "gate:approve")
	assert.Contains(t, keys, "run:read", "gate:approve should imply run:read")
	assert.Contains(t, keys, "project:read", "gate:approve should imply project:read")
}

func TestExpandImplications_GateReject(t *testing.T) {
	explicit := []Permission{{ObjGate, ActReject}}
	expanded := ExpandImplications(explicit)

	keys := permKeys(expanded)
	assert.Contains(t, keys, "gate:reject")
	assert.Contains(t, keys, "run:read")
	assert.Contains(t, keys, "project:read")
}

func TestExpandImplications_RunTrigger(t *testing.T) {
	explicit := []Permission{{ObjRun, ActTrigger}}
	expanded := ExpandImplications(explicit)

	keys := permKeys(expanded)
	assert.Contains(t, keys, "run:trigger")
	assert.Contains(t, keys, "project:read")
	assert.NotContains(t, keys, "run:read")
}

func TestExpandImplications_RunCancel(t *testing.T) {
	explicit := []Permission{{ObjRun, ActCancel}}
	expanded := ExpandImplications(explicit)

	keys := permKeys(expanded)
	assert.Contains(t, keys, "run:cancel")
	assert.Contains(t, keys, "run:read")
	assert.Contains(t, keys, "project:read")
}

func TestExpandImplications_ProjectWrite(t *testing.T) {
	explicit := []Permission{{ObjProject, ActWrite}}
	expanded := ExpandImplications(explicit)

	keys := permKeys(expanded)
	assert.Contains(t, keys, "project:write")
	assert.Contains(t, keys, "project:read")
}

func TestExpandImplications_AdminManageImpliesRead(t *testing.T) {
	adminObjs := []string{ObjWorkspace, ObjTeam, ObjEnvironment, ObjRunner, ObjConnection, ObjAPIKey, ObjSecret, ObjRole}
	for _, obj := range adminObjs {
		explicit := []Permission{{obj, ActManage}}
		expanded := ExpandImplications(explicit)
		keys := permKeys(expanded)
		assert.Contains(t, keys, obj+":manage")
		assert.Contains(t, keys, obj+":read", "%s:manage should imply %s:read", obj, obj)
	}
}

func TestExpandImplications_NoImplication(t *testing.T) {
	explicit := []Permission{{ObjProject, ActRead}}
	expanded := ExpandImplications(explicit)

	require.Len(t, expanded, 1)
	assert.Equal(t, "project:read", expanded[0].Key())
}

func TestExpandImplications_Deduplication(t *testing.T) {
	explicit := []Permission{
		{ObjGate, ActApprove},
		{ObjRun, ActTrigger},
	}
	expanded := ExpandImplications(explicit)

	keys := permKeys(expanded)
	count := 0
	for _, k := range keys {
		if k == "project:read" {
			count++
		}
	}
	assert.Equal(t, 1, count, "project:read should not be duplicated")
}

func TestIsWildcard(t *testing.T) {
	assert.True(t, IsWildcard([]Permission{{ObjWildcard, ActWildcard}}))
	assert.False(t, IsWildcard([]Permission{{ObjProject, ActRead}}))
	assert.False(t, IsWildcard(nil))
}

func TestIntersectScope(t *testing.T) {
	tests := []struct {
		name     string
		role     []string
		key      []string
		expected []string
	}{
		{"both empty", nil, nil, nil},
		{"role empty key set", nil, []string{"production"}, []string{"production"}},
		{"role set key empty", []string{"production", "staging"}, nil, []string{"production", "staging"}},
		{"intersection", []string{"production", "staging"}, []string{"production"}, []string{"production"}},
		{"no overlap", []string{"production"}, []string{"staging"}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := intersectScope(tt.role, tt.key)
			if tt.expected == nil {
				assert.Nil(t, result)
			} else {
				sort.Strings(result)
				sort.Strings(tt.expected)
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestSplitPermKey(t *testing.T) {
	obj, act := splitPermKey("gate:approve")
	assert.Equal(t, "gate", obj)
	assert.Equal(t, "approve", act)

	obj, act = splitPermKey("*:*")
	assert.Equal(t, "*", obj)
	assert.Equal(t, "*", act)
}

func permKeys(perms []Permission) []string {
	keys := make([]string, len(perms))
	for i, p := range perms {
		keys[i] = p.Key()
	}
	return keys
}
