// Package compute defines Flint's compute-provider contract: the small
// interface anything capable of producing machines implements to put itself
// under Flint. A "provider" doesn't have to be a cloud — a homelab script, a
// GPU broker, or a bare-metal fleet qualifies. The hard contract with Flint
// is minimal: given a Create call and a bootstrap token, a flint-agent must
// register with that token before the boot deadline, or the machine request
// failed.
//
// Quote is the piece that feeds Flint's economics: providers return priced
// offers (instance shape, $/hour, expected boot latency, interruption risk)
// and the fleet manager picks across them — potentially across providers —
// according to the pool's declared objective.
package compute

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// CapacityType selects the purchase model for elastic capacity.
type CapacityType string

const (
	CapacitySpot     CapacityType = "spot"
	CapacityOnDemand CapacityType = "on_demand"
	CapacityAny      CapacityType = "any"
)

// Sentinel errors providers use to declare capabilities they don't have.
var (
	// ErrUnsupported is returned by lifecycle methods a provider cannot
	// perform (e.g. Create on a static pool whose machines join by token).
	ErrUnsupported = errors.New("compute: operation not supported by this provider")
	// ErrNotReconcilable is returned by List when the provider has no
	// authoritative machine inventory to reconcile against (static pools).
	ErrNotReconcilable = errors.New("compute: provider has no reconcilable inventory")
)

// GPU describes an accelerator requirement.
type GPU struct {
	Vendor string `json:"vendor"` // e.g. "nvidia"
	Model  string `json:"model"`  // e.g. "t4", "a100"
	Count  int    `json:"count"`
}

// Requirements is what the fleet manager needs a machine to satisfy. Derived
// from the pool's shape/allow-lists plus pending assignment demand.
type Requirements struct {
	CPUMillis int64  `json:"cpuMillis"`
	MemoryMB  int64  `json:"memoryMb"`
	DiskGB    int64  `json:"diskGb"`
	Arch      string `json:"arch"` // "amd64" | "arm64"
	GPU       *GPU   `json:"gpu,omitempty"`
	// Regions is a preference order; empty means the provider default.
	Regions  []string     `json:"regions,omitempty"`
	Capacity CapacityType `json:"capacity"`
	// MaxBootSeconds bounds acceptable boot latency (0 = unconstrained).
	MaxBootSeconds int `json:"maxBootSeconds,omitempty"`
	// InstanceTypes optionally restricts offers to this allow-list.
	InstanceTypes []string `json:"instanceTypes,omitempty"`
	// Labels are capabilities a machine must carry (matched at registration).
	Labels map[string]string `json:"labels,omitempty"`
}

// Offer is one priced way a provider can satisfy Requirements. The fleet
// manager ranks offers by the pool's objective and records the chosen offer —
// and the rejected alternatives — in the decision ledger.
type Offer struct {
	Provider     string       `json:"provider"`
	InstanceType string       `json:"instanceType"`
	Region       string       `json:"region"`
	Zone         string       `json:"zone,omitempty"`
	Capacity     CapacityType `json:"capacity"`

	CPUMillis int64  `json:"cpuMillis"`
	MemoryMB  int64  `json:"memoryMb"`
	DiskGB    int64  `json:"diskGb"`
	Arch      string `json:"arch"`

	PricePerHourUSD     float64 `json:"pricePerHourUsd"`
	ExpectedBootSeconds int     `json:"expectedBootSeconds"`
	// InterruptionRisk is the provider's estimate of reclaim probability for
	// spot capacity, 0..1 (0 for on-demand).
	InterruptionRisk float64 `json:"interruptionRisk"`

	// ExpiresAt bounds quote validity (spot prices drift).
	ExpiresAt time.Time `json:"expiresAt"`

	// Payload is provider-opaque launch context (AMI, subnet, launch params)
	// echoed back verbatim on Create.
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Bootstrap carries what a freshly created machine needs to become a Flint
// agent: where to register and the one-time token that authenticates it.
type Bootstrap struct {
	ServerGRPCURL string `json:"serverGrpcUrl"`
	ServerHTTPURL string `json:"serverHttpUrl"`
	// MachineID is the pre-allocated machines row id; Create is idempotent per
	// MachineID (client-token semantics), so a crash after Create is safe to
	// retry without leaking instances.
	MachineID string `json:"machineId"`
	// RegistrationToken is the one-time plaintext bootstrap token (only its
	// hash is stored server-side; cleared on registration).
	RegistrationToken string `json:"registrationToken"`
	PoolName          string `json:"poolName"`
	// AgentDownloadURL serves the static linux flint-agent binary per arch.
	AgentDownloadURL string `json:"agentDownloadUrl"`
}

// RefState is the provider-side lifecycle of a machine reference.
type RefState string

const (
	RefPending    RefState = "pending"
	RefRunning    RefState = "running"
	RefStopping   RefState = "stopping"
	RefTerminated RefState = "terminated"
	RefUnknown    RefState = "unknown"
)

// MachineRef identifies a provider-side machine for Destroy/List.
type MachineRef struct {
	Provider string `json:"provider"`
	ID       string `json:"id"` // provider instance id (e.g. i-0abc…)
	// MachineID is the Flint machines-row id this instance was created for,
	// recovered from the provider's instance tag/label in List (and echoed by
	// Create). Reconciliation correlates on it so an instance Flint created but
	// hasn't finished recording (its provider_ref not yet committed) is never
	// mistaken for a zombie and destroyed. Empty when a provider can't surface
	// it — such instances fall back to the sighting-grace path.
	MachineID string   `json:"machineId,omitempty"`
	State     RefState `json:"state"`
}

// Provider is the contract compute integrations implement. Keep it brutally
// small: machine lifecycle, not a cloud SDK abstraction.
type Provider interface {
	// Name is the configured instance name ("aws-us-east-1"), not the type.
	Name() string

	// Quote returns zero or more offers satisfying req. Best-first ordering is
	// NOT required — the fleet manager ranks. A provider with no elastic
	// capacity (static pools) returns (nil, nil).
	Quote(ctx context.Context, req Requirements) ([]Offer, error)

	// Create launches a machine for a previously returned offer, arranging for
	// flint-agent to start with the bootstrap material. Idempotent per
	// bootstrap.MachineID. Static providers return ErrUnsupported.
	Create(ctx context.Context, offer Offer, bootstrap Bootstrap) (MachineRef, error)

	// Destroy terminates the machine. Idempotent; an unknown ref is success.
	Destroy(ctx context.Context, ref MachineRef) error

	// List returns the provider's current Flint-managed machine inventory —
	// the reconciliation source of truth for zombies and externally-killed
	// instances. Providers without an inventory return ErrNotReconcilable.
	List(ctx context.Context) ([]MachineRef, error)
}
