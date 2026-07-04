package runner

import (
	"context"
	"encoding/json"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/pkg/units"
)

// SpecFromRow converts a machine_pools DB row into a PoolSpec. This is the
// bridge that makes the DB the source of truth — pools are loaded from here
// into the in-memory registry (DB/API is canonical; no CRDs, no controllers).
func SpecFromRow(r db.ListMachinePoolsRow) PoolSpec {
	spec := PoolSpec{
		ID:          r.ID,
		Name:        r.Name,
		Description: derefStr(r.Description),
		Provider:    r.Provider,
		Arch:        r.Arch,
		Resources: ResourceProfile{
			// Optional — blank cpu/memory means the pool stamps no default and the
			// job sizes itself. Zero fields are treated as "unset" downstream.
			CPUMillis: optCPUMillis(r.Cpu),
			MemoryMB:  optMemoryMB(r.Memory),
		},
		DiskGB:         optDiskGB(derefStr(r.Disk)),
		InstanceTypes:  r.InstanceTypes,
		Regions:        r.Regions,
		DefaultTimeout: derefStr(r.DefaultTimeout),
		IsDefault:      r.IsDefault,
		Policy: Policy{
			CapacityType: r.CapacityType,
			Objective:    r.Objective,
			MinWarm:      int(r.MinWarm),
			MaxMachines:  int(r.MaxMachines),
			IdleTTL:      time.Duration(r.IdleTtlSeconds) * time.Second,
		},
	}

	if len(r.Overrides) > 0 {
		// Invalid overrides JSON degrades to "no overrides" rather than failing the
		// whole registry load; the API validates on write, so this only guards
		// hand-edited rows.
		_ = json.Unmarshal(r.Overrides, &spec.Policy.Overrides)
	}

	if f, err := r.HourlyCost.Float64Value(); err == nil && f.Valid {
		spec.HourlyCostUSD = f.Float64
	}

	if r.GpuVendor != nil && *r.GpuVendor != "" {
		spec.Resources.GPU = &GPURequest{
			Vendor: *r.GpuVendor,
			Model:  derefStr(r.GpuModel),
			Count:  int(r.GpuCount.Int32),
		}
		if spec.Resources.GPU.Count == 0 {
			spec.Resources.GPU.Count = 1
		}
	}

	return spec
}

// LoadAll loads every ready pool from the DB into the registry, replacing its
// contents. Called at boot and on refresh.
func LoadAll(ctx context.Context, q db.Querier, reg *Registry) error {
	rows, err := q.ListMachinePools(ctx)
	if err != nil {
		return err
	}
	specs := make([]PoolSpec, 0, len(rows))
	for _, row := range rows {
		specs = append(specs, SpecFromRow(row))
		// The DB-marked default takes precedence over the config fallback. Pools
		// with no runner: resolve to it.
		if row.IsDefault {
			SetDefault(row.Name)
		}
	}
	reg.ReplaceAll(specs)
	return nil
}

// optCPUMillis parses a CPU quantity, returning 0 (treated as "unset") when the
// string is blank or invalid.
func optCPUMillis(s string) int64 {
	if s == "" {
		return 0
	}
	m, err := units.ParseCPUMillis(s)
	if err != nil {
		return 0
	}
	return m
}

func optMemoryMB(s string) int64 {
	if s == "" {
		return 0
	}
	mb, err := units.ParseMemoryMB(s)
	if err != nil {
		return 0
	}
	return mb
}

func optDiskGB(s string) int64 {
	if s == "" {
		return 0
	}
	gb, err := units.ParseDiskGB(s)
	if err != nil {
		return 0
	}
	return gb
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
