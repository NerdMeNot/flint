// Package runner handles machine-pool resolution — translating developer-facing
// pool names and t-shirt sizes into machine shape requirements and fleet policy.
//
// Developers write: runner: standard
// Platform teams define: machine pools (DB/API-managed) with a compute provider,
// default machine shape, and economics policy (min warm, idle TTL, objective).
// This package bridges the two.
package runner

import (
	"time"

	"github.com/NerdMeNot/flint/pkg/units"
)

// PoolSpec is the resolved specification for a machine pool: which provider
// supplies machines, what shape they take, and the economics policy the fleet
// manager enforces for them.
type PoolSpec struct {
	ID          string
	Name        string
	Description string

	// Provider is the compute_providers.name that supplies this pool's machines.
	// "static" pools have no elastic capacity — machines join via the pool join
	// token instead of provider.Create.
	Provider string

	Arch string

	// Resources is the pool's default machine shape. Elastic pools use it as the
	// Quote requirements floor; static pools as validation bounds; steps without
	// explicit resources: inherit it as their requirement.
	Resources ResourceProfile
	DiskGB    int64

	// InstanceTypes / Regions optionally narrow what Quote may offer.
	InstanceTypes []string
	Regions       []string

	Policy Policy

	// HourlyCostUSD is the optional operator-declared amortized cost for static
	// machines with no offer price (0 = unset; the costs config fallback applies).
	HourlyCostUSD float64

	DefaultTimeout string
	IsDefault      bool
}

// Policy is the pool's economics policy — the user states the tradeoff, the
// fleet manager optimizes within it.
type Policy struct {
	// CapacityType: "spot" | "on_demand" | "any".
	CapacityType string
	// Objective ranks quotes when provisioning: "cost" | "latency" | "balanced".
	Objective string
	// MinWarm machines are kept alive regardless of idle TTL. 0 = zero standing
	// infra: the first run after idle pays the boot latency.
	MinWarm int
	// MaxMachines caps the pool's live machine count.
	MaxMachines int
	// IdleTTL is how long an idle machine (above MinWarm) survives before drain.
	IdleTTL time.Duration
	// Overrides adjust the policy per branch/event (e.g. main warm, PRs cold spot).
	Overrides []PolicyOverride
}

// PolicyOverride is one per-branch/event policy adjustment.
type PolicyOverride struct {
	Match PolicyMatch `json:"match"`
	Set   PolicyPatch `json:"set"`
}

// PolicyMatch selects the runs an override applies to. Empty fields match all.
type PolicyMatch struct {
	Branch string `json:"branch,omitempty"`
	Event  string `json:"event,omitempty"`
}

// PolicyPatch holds the policy fields an override may change; nil = unchanged.
type PolicyPatch struct {
	MinWarm      *int    `json:"minWarm,omitempty"`
	CapacityType *string `json:"capacityType,omitempty"`
	Objective    *string `json:"objective,omitempty"`
	IdleTTLSecs  *int    `json:"idleTtlSeconds,omitempty"`
}

// ResourceProfile defines compute resources as plain integers (millicores /
// mebibytes) so schedulers and providers compare without unit parsing.
type ResourceProfile struct {
	CPUMillis int64
	MemoryMB  int64
	GPU       *GPURequest
}

// GPURequest specifies GPU or accelerator requirements.
type GPURequest struct {
	Vendor string // e.g., "nvidia", "amd"
	Model  string // e.g., "t4", "a100"
	Count  int
}

// ParseResources converts quantity strings ("500m", "8Gi") into a profile.
// Blank strings yield zero fields, meaning "unset" (the pool stamps no value).
func ParseResources(cpu, memory string) (ResourceProfile, error) {
	var p ResourceProfile
	if cpu != "" {
		m, err := units.ParseCPUMillis(cpu)
		if err != nil {
			return p, err
		}
		p.CPUMillis = m
	}
	if memory != "" {
		mb, err := units.ParseMemoryMB(memory)
		if err != nil {
			return p, err
		}
		p.MemoryMB = mb
	}
	return p, nil
}
