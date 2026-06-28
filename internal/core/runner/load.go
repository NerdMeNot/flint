package runner

import (
	"context"
	"encoding/json"

	"github.com/NerdMeNot/flint/internal/core/db"
	"k8s.io/apimachinery/pkg/api/resource"
)

// SpecFromRow converts a runner_pools DB row into a PoolSpec. This is the bridge
// that makes the DB the source of truth — the worker loads pools from here into
// the in-memory registry (no CRD/controller).
func SpecFromRow(r db.ListRunnerPoolsRow) PoolSpec {
	spec := PoolSpec{
		Name:               r.Name,
		RunAsNonRoot:       r.RunAsNonRoot,
		ServiceAccountName: derefStr(r.ServiceAccountName),
		Resources: ResourceProfile{
			// Optional — a blank cpu/memory means the pool stamps no request for it
			// (the job sizes itself). Left as the zero Quantity, which MergeIntoJob skips.
			CPU:    optQty(r.Cpu),
			Memory: optQty(r.Memory),
		},
		Workspace: WorkspaceConfig{
			Mode:         WorkspaceMode(orStr(r.WorkspaceMode, string(WorkspaceModeAgent))),
			StorageClass: derefStr(r.WorkspaceStorageClass),
			Size:         orStr(r.WorkspaceSize, "10Gi"),
		},
	}
	spec.Description = derefStr(r.Description)

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

	// Arch is a descriptor (catalog, validation, and — for managed pools — the
	// rendered Karpenter NodePool requirement); it is NOT auto-pinned into the pod
	// nodeSelector. A reference pool only constrains arch if the author explicitly
	// added kubernetes.io/arch to its node selector.
	if len(r.NodeSelector) > 0 {
		_ = json.Unmarshal(r.NodeSelector, &spec.NodeSelector)
	}
	if len(r.Tolerations) > 0 {
		_ = json.Unmarshal(r.Tolerations, &spec.Tolerations)
	}
	return spec
}

// LoadAll loads every ready pool from the DB into the registry, replacing its
// contents. Called by the worker at boot and on change.
func LoadAll(ctx context.Context, q db.Querier, reg *Registry) error {
	rows, err := q.ListRunnerPools(ctx)
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

// optQty parses a resource quantity, returning the zero Quantity (treated as
// "unset" by MergeIntoJob) when the string is blank or invalid.
func optQty(s string) resource.Quantity {
	if s == "" {
		return resource.Quantity{}
	}
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return resource.Quantity{}
	}
	return q
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func orStr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
