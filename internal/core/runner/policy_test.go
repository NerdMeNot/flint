package runner

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func intp(i int) *int       { return &i }
func strp(s string) *string { return &s }

func basePolicy() Policy {
	return Policy{
		CapacityType: "spot",
		Objective:    "cost",
		MinWarm:      0,
		MaxMachines:  10,
		IdleTTL:      5 * time.Minute,
	}
}

func TestResolvePolicy(t *testing.T) {
	tests := []struct {
		name          string
		overrides     []PolicyOverride
		branch, event string
		want          Policy
	}{
		{
			name:      "no overrides returns base unchanged",
			overrides: nil,
			branch:    "main", event: "push",
			want: basePolicy(),
		},
		{
			name: "branch match applies the patch (main warm + on-demand)",
			overrides: []PolicyOverride{
				{Match: PolicyMatch{Branch: "main"}, Set: PolicyPatch{
					MinWarm: intp(2), CapacityType: strp("on_demand"),
				}},
			},
			branch: "main", event: "push",
			want: func() Policy { p := basePolicy(); p.MinWarm = 2; p.CapacityType = "on_demand"; return p }(),
		},
		{
			name: "non-matching branch leaves base unchanged",
			overrides: []PolicyOverride{
				{Match: PolicyMatch{Branch: "main"}, Set: PolicyPatch{MinWarm: intp(2)}},
			},
			branch: "feature/x", event: "push",
			want: basePolicy(),
		},
		{
			name: "event match (PRs stay cold spot)",
			overrides: []PolicyOverride{
				{Match: PolicyMatch{Event: "pull_request"}, Set: PolicyPatch{
					MinWarm: intp(0), CapacityType: strp("spot"), Objective: strp("cost"),
				}},
			},
			branch: "feature/x", event: "pull_request",
			want: basePolicy(), // already cold spot, patch confirms it
		},
		{
			name: "branch AND event must both match",
			overrides: []PolicyOverride{
				{Match: PolicyMatch{Branch: "main", Event: "push"}, Set: PolicyPatch{MinWarm: intp(3)}},
			},
			branch: "main", event: "manual",
			want: basePolicy(), // event mismatch → no apply
		},
		{
			name: "first match wins — specific before catch-all",
			overrides: []PolicyOverride{
				{Match: PolicyMatch{Branch: "main"}, Set: PolicyPatch{MinWarm: intp(5)}},
				{Match: PolicyMatch{}, Set: PolicyPatch{MinWarm: intp(1)}}, // catch-all
			},
			branch: "main", event: "push",
			want: func() Policy { p := basePolicy(); p.MinWarm = 5; return p }(),
		},
		{
			name: "catch-all (empty match) applies when nothing specific matched",
			overrides: []PolicyOverride{
				{Match: PolicyMatch{Branch: "main"}, Set: PolicyPatch{MinWarm: intp(5)}},
				{Match: PolicyMatch{}, Set: PolicyPatch{MinWarm: intp(1)}},
			},
			branch: "dev", event: "push",
			want: func() Policy { p := basePolicy(); p.MinWarm = 1; return p }(),
		},
		{
			name: "IdleTTL patch is seconds → duration",
			overrides: []PolicyOverride{
				{Match: PolicyMatch{Branch: "main"}, Set: PolicyPatch{IdleTTLSecs: intp(120)}},
			},
			branch: "main", event: "push",
			want: func() Policy { p := basePolicy(); p.IdleTTL = 2 * time.Minute; return p }(),
		},
		{
			name: "nil patch fields leave the corresponding base field untouched",
			overrides: []PolicyOverride{
				{Match: PolicyMatch{Branch: "main"}, Set: PolicyPatch{Objective: strp("latency")}},
			},
			branch: "main", event: "push",
			// only Objective changes; MinWarm/CapacityType/IdleTTL stay at base.
			want: func() Policy { p := basePolicy(); p.Objective = "latency"; return p }(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := basePolicy()
			base.Overrides = tt.overrides
			got := ResolvePolicy(base, tt.branch, tt.event)

			// The resolved policy never carries overrides forward (avoids double
			// application downstream).
			assert.Nil(t, got.Overrides, "resolved policy must not carry overrides")

			// Compare on the effective fields (ignore Overrides, cleared above).
			tt.want.Overrides = nil
			assert.Equal(t, tt.want, got)
		})
	}
}

// ResolvePolicy must not mutate the base policy or its overrides slice.
func TestResolvePolicy_DoesNotMutateBase(t *testing.T) {
	base := basePolicy()
	base.Overrides = []PolicyOverride{
		{Match: PolicyMatch{Branch: "main"}, Set: PolicyPatch{MinWarm: intp(9)}},
	}
	_ = ResolvePolicy(base, "main", "push")
	assert.Equal(t, 0, base.MinWarm, "base MinWarm untouched")
	assert.Len(t, base.Overrides, 1, "base overrides untouched")
}

// An empty run context (unknown branch/event) matches only wildcard overrides.
func TestResolvePolicy_UnknownRunContext(t *testing.T) {
	base := basePolicy()
	base.Overrides = []PolicyOverride{
		{Match: PolicyMatch{Branch: "main"}, Set: PolicyPatch{MinWarm: intp(5)}},
		{Match: PolicyMatch{}, Set: PolicyPatch{MinWarm: intp(1)}},
	}
	got := ResolvePolicy(base, "", "")
	assert.Equal(t, 1, got.MinWarm, "unknown context falls through to the wildcard override")
}
