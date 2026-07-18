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
