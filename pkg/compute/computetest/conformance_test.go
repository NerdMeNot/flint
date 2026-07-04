package computetest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/compute"
	_ "github.com/NerdMeNot/flint/pkg/compute/staticpool"
)

// The Fake itself must pass the conformance suite — it is what fleet tests
// trust as a stand-in for real clouds.
func TestFake_Conformance(t *testing.T) {
	RunConformance(t, func(t *testing.T) compute.Provider {
		return NewFake("fake-test")
	})
}

// The static provider passes via the static branch.
func TestStatic_Conformance(t *testing.T) {
	RunConformance(t, func(t *testing.T) compute.Provider {
		p, err := compute.New(context.Background(), "static", "static", nil, nil)
		require.NoError(t, err)
		return p
	})
}

func TestFake_SpotAndOnDemandOffers(t *testing.T) {
	f := NewFake("fake")
	offers, err := f.Quote(context.Background(), compute.Requirements{
		CPUMillis: 2000, MemoryMB: 4096, Arch: "amd64", Capacity: compute.CapacityAny,
	})
	require.NoError(t, err)

	var spot, od int
	for _, o := range offers {
		switch o.Capacity {
		case compute.CapacitySpot:
			spot++
			assert.Greater(t, o.InterruptionRisk, 0.0)
		case compute.CapacityOnDemand:
			od++
			assert.Zero(t, o.InterruptionRisk)
		}
	}
	assert.Greater(t, spot, 0, "capacity=any should include spot offers")
	assert.Greater(t, od, 0, "capacity=any should include on-demand offers")
}

func TestFake_OnCreateHookAndKill(t *testing.T) {
	f := NewFake("fake")
	var created []FakeMachine
	f.OnCreate = func(m FakeMachine) { created = append(created, m) }

	offers, err := f.Quote(context.Background(), compute.Requirements{CPUMillis: 1000, MemoryMB: 1024, Arch: "amd64", Capacity: compute.CapacitySpot})
	require.NoError(t, err)
	require.NotEmpty(t, offers)

	ref, err := f.Create(context.Background(), offers[0], compute.Bootstrap{MachineID: "m-1", RegistrationToken: "tok"})
	require.NoError(t, err)
	require.Len(t, created, 1)
	assert.Equal(t, "tok", created[0].Bootstrap.RegistrationToken)

	// Spot reclaim: machine vanishes without Flint asking.
	assert.True(t, f.KillMachine(ref.ID))
	assert.False(t, f.KillMachine(ref.ID), "already dead")

	refs, err := f.List(context.Background())
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, compute.RefTerminated, refs[0].State)
}

func TestRegistry_UnknownType(t *testing.T) {
	_, err := compute.New(context.Background(), "nope", "x", json.RawMessage(`{}`), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown provider type")
}
