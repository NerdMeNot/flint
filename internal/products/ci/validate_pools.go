package ci

import (
	"context"
	"fmt"
	"sort"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/pkg/units"
)

// ValidateWithPools runs structural Validate, then checks every job's runner pool
// and resource request against the catalog of pools in the DB — so an unknown
// pool, a GPU request on a non-GPU pool, or a request larger than the pool's
// machine shape fails at compile time with a named fix, instead of as a step
// waiting forever for capacity that can never exist.
//
// Only explicitly-named pools are checked (a job with no runner uses the configured
// default pool, resolved at dispatch). The Service calls this before Compile.
func (p *Pipeline) ValidateWithPools(ctx context.Context, q db.Querier) error {
	if err := p.Validate(); err != nil {
		return err
	}

	rows, err := q.ListMachinePools(ctx)
	if err != nil {
		return fmt.Errorf("ci: load runner pools: %w", err)
	}
	pools := make(map[string]db.ListMachinePoolsRow, len(rows))
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		pools[r.Name] = r
		names = append(names, r.Name)
	}
	sort.Strings(names)

	for _, name := range sortedJobNames(p.Jobs) {
		job := p.Jobs[name]
		poolName := firstNonEmpty(job.Runner, p.Runner)
		if poolName == "" {
			continue // uses the default pool — resolved at dispatch
		}
		pool, ok := pools[poolName]
		if !ok {
			return fmt.Errorf("ci: job %q references unknown runner pool %q (available: %v)", name, poolName, names)
		}
		if job.Resources != nil {
			if err := validateResourcesFitPool(name, job.Resources, pool); err != nil {
				return err
			}
		}
	}
	return nil
}

// validateResourcesFitPool checks a job's resource request against the pool's
// machine shape. A pool with a blank cpu/memory declares no bound for it.
func validateResourcesFitPool(jobName string, req *Resources, pool db.ListMachinePoolsRow) error {
	if req.CPU != "" {
		jobCPU, err := units.ParseCPUMillis(req.CPU)
		if err != nil {
			return fmt.Errorf("ci: job %q has invalid cpu %q", jobName, req.CPU)
		}
		if pool.Cpu != "" {
			if poolCPU, perr := units.ParseCPUMillis(pool.Cpu); perr == nil && jobCPU > poolCPU {
				return fmt.Errorf("ci: job %q requests cpu %s but pool %q provides %s", jobName, req.CPU, pool.Name, pool.Cpu)
			}
		}
	}
	if req.Memory != "" {
		jobMem, err := units.ParseMemoryMB(req.Memory)
		if err != nil {
			return fmt.Errorf("ci: job %q has invalid memory %q", jobName, req.Memory)
		}
		if pool.Memory != "" {
			if poolMem, perr := units.ParseMemoryMB(pool.Memory); perr == nil && jobMem > poolMem {
				return fmt.Errorf("ci: job %q requests memory %s but pool %q provides %s", jobName, req.Memory, pool.Name, pool.Memory)
			}
		}
	}
	if req.GPU > 0 {
		if pool.GpuVendor == nil || *pool.GpuVendor == "" || !pool.GpuCount.Valid || pool.GpuCount.Int32 == 0 {
			return fmt.Errorf("ci: job %q requests %d GPU but pool %q has no GPUs", jobName, req.GPU, pool.Name)
		}
		if int32(req.GPU) > pool.GpuCount.Int32 {
			return fmt.Errorf("ci: job %q requests %d GPU but pool %q provides %d", jobName, req.GPU, pool.Name, pool.GpuCount.Int32)
		}
	}
	return nil
}
