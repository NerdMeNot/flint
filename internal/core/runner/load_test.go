package runner

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/internal/core/db"
)

func TestSpecFromRow(t *testing.T) {
	disk := "100Gi"
	timeout := "30m"
	desc := "general builds"
	overrides, _ := json.Marshal([]PolicyOverride{
		{Match: PolicyMatch{Branch: "main"}, Set: PolicyPatch{MinWarm: intPtr(1)}},
	})

	spec := SpecFromRow(db.ListMachinePoolsRow{
		ID:             "pool-1",
		Name:           "standard",
		Description:    &desc,
		Provider:       "aws-us-east-1",
		Arch:           "arm64",
		Cpu:            "4",
		Memory:         "8Gi",
		Disk:           &disk,
		InstanceTypes:  []string{"c7g.xlarge", "c7g.2xlarge"},
		Regions:        []string{"us-east-1"},
		CapacityType:   "spot",
		Objective:      "cost",
		MinWarm:        1,
		MaxMachines:    20,
		IdleTtlSeconds: 600,
		Overrides:      overrides,
		DefaultTimeout: &timeout,
		IsDefault:      true,
	})

	assert.Equal(t, "pool-1", spec.ID)
	assert.Equal(t, "standard", spec.Name)
	assert.Equal(t, "aws-us-east-1", spec.Provider)
	assert.Equal(t, "arm64", spec.Arch)
	assert.Equal(t, int64(4000), spec.Resources.CPUMillis)
	assert.Equal(t, int64(8192), spec.Resources.MemoryMB)
	assert.Equal(t, int64(100), spec.DiskGB)
	assert.Equal(t, []string{"c7g.xlarge", "c7g.2xlarge"}, spec.InstanceTypes)
	assert.Equal(t, "spot", spec.Policy.CapacityType)
	assert.Equal(t, "cost", spec.Policy.Objective)
	assert.Equal(t, 1, spec.Policy.MinWarm)
	assert.Equal(t, 20, spec.Policy.MaxMachines)
	assert.Equal(t, 10*time.Minute, spec.Policy.IdleTTL)
	assert.Equal(t, "30m", spec.DefaultTimeout)
	assert.True(t, spec.IsDefault)

	require.Len(t, spec.Policy.Overrides, 1)
	assert.Equal(t, "main", spec.Policy.Overrides[0].Match.Branch)
	require.NotNil(t, spec.Policy.Overrides[0].Set.MinWarm)
	assert.Equal(t, 1, *spec.Policy.Overrides[0].Set.MinWarm)
}

func TestSpecFromRow_OptionalResources(t *testing.T) {
	t.Run("blank cpu/memory leave zero profile", func(t *testing.T) {
		spec := SpecFromRow(db.ListMachinePoolsRow{Name: "p", Arch: "amd64", Cpu: "", Memory: ""})
		assert.Zero(t, spec.Resources.CPUMillis)
		assert.Zero(t, spec.Resources.MemoryMB)
	})

	t.Run("invalid quantities degrade to unset rather than failing the load", func(t *testing.T) {
		spec := SpecFromRow(db.ListMachinePoolsRow{Name: "p", Arch: "amd64", Cpu: "garbage", Memory: "junk"})
		assert.Zero(t, spec.Resources.CPUMillis)
		assert.Zero(t, spec.Resources.MemoryMB)
	})
}

func TestSpecFromRow_GPUDefaultsCountToOne(t *testing.T) {
	vendor := "nvidia"
	spec := SpecFromRow(db.ListMachinePoolsRow{Name: "gpu", Arch: "amd64", Cpu: "8", Memory: "32Gi", GpuVendor: &vendor})
	require.NotNil(t, spec.Resources.GPU)
	assert.Equal(t, 1, spec.Resources.GPU.Count)
}

func intPtr(i int) *int { return &i }
