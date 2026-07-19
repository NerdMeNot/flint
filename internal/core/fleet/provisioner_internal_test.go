package fleet

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/pkg/compute"
)

// A8c: only offers whose quote validity has lapsed are dropped; a zero ExpiresAt
// means the provider makes no validity claim and is always kept.
func TestFreshOffers(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	offers := []compute.Offer{
		{InstanceType: "no-expiry"},                               // zero ExpiresAt → kept
		{InstanceType: "future", ExpiresAt: now.Add(time.Minute)}, // valid → kept
		{InstanceType: "past", ExpiresAt: now.Add(-time.Minute)},  // expired → dropped
	}
	fresh := freshOffers(offers, now)

	kept := map[string]bool{}
	for _, o := range fresh {
		kept[o.InstanceType] = true
	}
	assert.True(t, kept["no-expiry"], "an offer with no expiry is always fresh")
	assert.True(t, kept["future"], "an unexpired offer is kept")
	assert.False(t, kept["past"], "an expired offer is dropped")
	assert.Len(t, fresh, 2)

	// Boundary: an offer expiring exactly now is not yet valid in the future.
	edge := freshOffers([]compute.Offer{{ExpiresAt: now}}, now)
	assert.Empty(t, edge, "an offer expiring exactly at now is treated as stale")
}

// A8d: a pool that declares a GPU shape surfaces it in the Quote requirements,
// so the provider can't rank CPU-only offers for GPU work.
func TestRequirementsFromPool_PopulatesGPU(t *testing.T) {
	row := db.ListMachinePoolsRow{
		Arch: "amd64", CapacityType: "on_demand", Cpu: "4", Memory: "8Gi",
		GpuVendor: strp("nvidia"),
		GpuModel:  strp("a100"),
		GpuCount:  pgtype.Int4{Int32: 2, Valid: true},
	}
	req, err := requirementsFromPool(row)
	require.NoError(t, err)
	require.NotNil(t, req.GPU, "GPU requirement must be populated")
	assert.Equal(t, "nvidia", req.GPU.Vendor)
	assert.Equal(t, "a100", req.GPU.Model)
	assert.Equal(t, 2, req.GPU.Count)

	// A pool with no GPU vendor leaves the requirement nil (CPU-only).
	cpuOnly, err := requirementsFromPool(db.ListMachinePoolsRow{
		Arch: "amd64", CapacityType: "on_demand", Cpu: "2", Memory: "4Gi",
	})
	require.NoError(t, err)
	assert.Nil(t, cpuOnly.GPU)
}

// B1: resolveProvisionContext picks the dominant demand group and applies the
// pool's per-branch/event override to it. A base spot pool with a main→on_demand
// override boots stable when main demand dominates, spot otherwise.
func TestResolveProvisionContext_PerBranchClass(t *testing.T) {
	pool := db.ListMachinePoolsRow{
		CapacityType: "spot", Objective: "cost", MinWarm: 0, MaxMachines: 5,
		IdleTtlSeconds: 300,
		// main runs on stable (on_demand) with a latency objective; everything
		// else keeps the base spot/cost.
		Overrides: []byte(`[{"match":{"branch":"main"},"set":{"capacityType":"on_demand","objective":"latency"}}]`),
	}

	t.Run("main-dominant demand resolves the override", func(t *testing.T) {
		pc := resolveProvisionContext(pool, []db.PendingProvisioningDemandByGroupRow{
			{Branch: "main", Event: "push", N: 5},
			{Branch: "pr/7", Event: "pull_request", N: 2},
		})
		assert.Equal(t, compute.CapacityOnDemand, pc.capacity)
		assert.Equal(t, "latency", pc.objective)
		assert.Equal(t, "main", pc.branch)
		assert.True(t, pc.overrideApplied)
	})

	t.Run("PR-dominant demand keeps the base policy", func(t *testing.T) {
		pc := resolveProvisionContext(pool, []db.PendingProvisioningDemandByGroupRow{
			{Branch: "main", Event: "push", N: 1},
			{Branch: "pr/7", Event: "pull_request", N: 9},
		})
		assert.Equal(t, compute.CapacityType("spot"), pc.capacity)
		assert.Equal(t, "cost", pc.objective)
		assert.Equal(t, "pr/7", pc.branch)
		assert.False(t, pc.overrideApplied)
	})

	t.Run("no demand (warm-floor only) resolves the base against the empty context", func(t *testing.T) {
		pc := resolveProvisionContext(pool, nil)
		assert.Equal(t, compute.CapacityType("spot"), pc.capacity)
		assert.Equal(t, "cost", pc.objective)
		assert.Equal(t, "", pc.branch)
	})
}
