package computetest

import (
	"context"
	cryptorand "crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/NerdMeNot/flint/pkg/compute"
)

// FakeShape is one instance type the Fake can offer.
type FakeShape struct {
	InstanceType    string
	CPUMillis       int64
	MemoryMB        int64
	DiskGB          int64
	Arch            string
	OnDemandUSD     float64 // $/hour
	SpotUSD         float64 // $/hour; 0 = no spot market for this shape
	BootSeconds     int
	InterruptionPct float64 // 0..1 spot reclaim risk
}

// FakeMachine is the Fake's record of a created machine.
type FakeMachine struct {
	Ref       compute.MachineRef
	Offer     compute.Offer
	Bootstrap compute.Bootstrap
	CreatedAt time.Time
}

// Fake is an in-memory elastic provider for fleet-manager and conformance
// tests: configurable shapes, an OnCreate hook to drive a fake agent's
// registration, and spot-kill simulation via KillMachine.
type Fake struct {
	ProviderName string
	Region       string
	Shapes       []FakeShape

	// OnCreate, when set, is invoked synchronously after a successful Create —
	// tests use it to simulate the machine booting and the agent registering
	// with the bootstrap token.
	OnCreate func(m FakeMachine)

	// CreateErr, when set, makes Create fail (provider outage simulation).
	CreateErr error

	mu       sync.Mutex
	machines map[string]*FakeMachine // keyed by bootstrap.MachineID
	seq      int
}

// NewFake builds a Fake with a sensible default catalog (amd64 + arm64,
// on-demand + spot) under the given provider name.
func NewFake(name string) *Fake {
	return &Fake{
		ProviderName: name,
		Region:       "test-1",
		Shapes: []FakeShape{
			{InstanceType: "t.small", CPUMillis: 2000, MemoryMB: 4096, DiskGB: 50, Arch: "amd64", OnDemandUSD: 0.04, SpotUSD: 0.012, BootSeconds: 20, InterruptionPct: 0.05},
			{InstanceType: "t.large", CPUMillis: 4000, MemoryMB: 8192, DiskGB: 100, Arch: "amd64", OnDemandUSD: 0.08, SpotUSD: 0.025, BootSeconds: 25, InterruptionPct: 0.08},
			{InstanceType: "t.xlarge", CPUMillis: 8000, MemoryMB: 16384, DiskGB: 100, Arch: "amd64", OnDemandUSD: 0.16, SpotUSD: 0.05, BootSeconds: 30, InterruptionPct: 0.10},
			{InstanceType: "a.large", CPUMillis: 4000, MemoryMB: 8192, DiskGB: 100, Arch: "arm64", OnDemandUSD: 0.065, SpotUSD: 0.02, BootSeconds: 25, InterruptionPct: 0.06},
		},
		machines: map[string]*FakeMachine{},
	}
}

func (f *Fake) Name() string { return f.ProviderName }

// Quote prices every shape that satisfies the requirements, emitting both
// spot and on-demand offers when capacity is "any".
func (f *Fake) Quote(ctx context.Context, req compute.Requirements) ([]compute.Offer, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var offers []compute.Offer
	for _, s := range f.Shapes {
		if s.CPUMillis < req.CPUMillis || s.MemoryMB < req.MemoryMB || s.DiskGB < req.DiskGB {
			continue
		}
		if req.Arch != "" && s.Arch != req.Arch {
			continue
		}
		if req.MaxBootSeconds > 0 && s.BootSeconds > req.MaxBootSeconds {
			continue
		}
		if len(req.InstanceTypes) > 0 && !slices.Contains(req.InstanceTypes, s.InstanceType) {
			continue
		}
		mk := func(cap compute.CapacityType, price, risk float64) compute.Offer {
			return compute.Offer{
				Provider: f.ProviderName, InstanceType: s.InstanceType, Region: f.Region,
				Capacity: cap, CPUMillis: s.CPUMillis, MemoryMB: s.MemoryMB, DiskGB: s.DiskGB,
				Arch: s.Arch, PricePerHourUSD: price, ExpectedBootSeconds: s.BootSeconds,
				InterruptionRisk: risk, ExpiresAt: time.Now().Add(5 * time.Minute),
			}
		}
		wantSpot := req.Capacity == compute.CapacitySpot || req.Capacity == compute.CapacityAny
		wantOD := req.Capacity == compute.CapacityOnDemand || req.Capacity == compute.CapacityAny || req.Capacity == ""
		if wantSpot && s.SpotUSD > 0 {
			offers = append(offers, mk(compute.CapacitySpot, s.SpotUSD, s.InterruptionPct))
		}
		if wantOD {
			offers = append(offers, mk(compute.CapacityOnDemand, s.OnDemandUSD, 0))
		}
	}
	return offers, nil
}

// Create records the machine and fires OnCreate. Idempotent per MachineID.
func (f *Fake) Create(ctx context.Context, offer compute.Offer, bootstrap compute.Bootstrap) (compute.MachineRef, error) {
	if err := ctx.Err(); err != nil {
		return compute.MachineRef{}, err
	}
	if f.CreateErr != nil {
		return compute.MachineRef{}, f.CreateErr
	}
	f.mu.Lock()
	if existing, ok := f.machines[bootstrap.MachineID]; ok {
		ref := existing.Ref
		f.mu.Unlock()
		return ref, nil
	}
	f.seq++
	// Instance ids are globally unique (as real clouds guarantee): a
	// per-Fake sequence alone would collide across Fake instances sharing a
	// provider name in one database.
	nonce := make([]byte, 4)
	_, _ = cryptorand.Read(nonce)
	m := &FakeMachine{
		Ref: compute.MachineRef{
			Provider: f.ProviderName,
			ID: fmt.Sprintf("fake-%s-%d-%s",
				strings.ReplaceAll(offer.InstanceType, ".", "-"), f.seq, hex.EncodeToString(nonce)),
			MachineID: bootstrap.MachineID,
			State:     compute.RefRunning,
		},
		Offer: offer, Bootstrap: bootstrap, CreatedAt: time.Now(),
	}
	f.machines[bootstrap.MachineID] = m
	hook := f.OnCreate
	f.mu.Unlock()

	if hook != nil {
		hook(*m)
	}
	return m.Ref, nil
}

// Destroy marks the machine terminated. Unknown refs are success.
func (f *Fake) Destroy(ctx context.Context, ref compute.MachineRef) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.machines {
		if m.Ref.ID == ref.ID {
			m.Ref.State = compute.RefTerminated
		}
	}
	return nil
}

// List returns the current inventory including terminated refs (as a real
// cloud briefly does), letting reconciliation tests see both states.
func (f *Fake) List(ctx context.Context) ([]compute.MachineRef, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	refs := make([]compute.MachineRef, 0, len(f.machines))
	for _, m := range f.machines {
		refs = append(refs, m.Ref)
	}
	return refs, nil
}

// KillMachine simulates a spot reclaim / external termination: the machine
// disappears from the provider without Flint asking. Returns false if no
// machine with that ref id exists.
func (f *Fake) KillMachine(refID string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.machines {
		if m.Ref.ID == refID && m.Ref.State != compute.RefTerminated {
			m.Ref.State = compute.RefTerminated
			return true
		}
	}
	return false
}

// Machines snapshots the created machines (test assertions).
func (f *Fake) Machines() []FakeMachine {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]FakeMachine, 0, len(f.machines))
	for _, m := range f.machines {
		out = append(out, *m)
	}
	return out
}
