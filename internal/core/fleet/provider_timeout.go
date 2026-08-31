package fleet

import (
	"context"
	"time"

	"github.com/NerdMeNot/flint/pkg/compute"
)

// ProviderTimeouts bounds a SINGLE compute-provider API call. Every provider
// method reaches a network Flint does not control, so an unbounded call is an
// unbounded stall: the capacity loop is one goroutine, and a hung Quote parks
// provisioning and scale-down for the entire fleet until the process restarts.
//
// These bound the API call, NOT the machine's boot — boot latency is bounded
// separately by boot_deadline_at and swept by ExpireBootDeadlines.
type ProviderTimeouts struct {
	Quote   time.Duration
	Create  time.Duration
	Destroy time.Duration
	List    time.Duration
}

// DefaultProviderTimeouts are deliberately generous. Their job is to turn a
// hang into an error, not to police a slow-but-working cloud: a timeout on
// Create can leak a real instance (the launch may have landed after we gave up),
// so cutting one short costs money. Create/Destroy get the longest budget
// because they are the calls that mutate provider state.
func DefaultProviderTimeouts() ProviderTimeouts {
	return ProviderTimeouts{
		Quote:   30 * time.Second,
		Create:  2 * time.Minute,
		Destroy: 2 * time.Minute,
		List:    60 * time.Second,
	}
}

// withDefaults fills unset (non-positive) fields from the defaults, so a caller
// can override one timeout without restating the rest.
func (t ProviderTimeouts) withDefaults() ProviderTimeouts {
	d := DefaultProviderTimeouts()
	if t.Quote <= 0 {
		t.Quote = d.Quote
	}
	if t.Create <= 0 {
		t.Create = d.Create
	}
	if t.Destroy <= 0 {
		t.Destroy = d.Destroy
	}
	if t.List <= 0 {
		t.List = d.List
	}
	return t
}

// SetProviderTimeouts overrides the per-call provider timeouts. Unset fields
// keep their defaults. Must be called before the fleet loop starts.
func (f *Fleet) SetProviderTimeouts(t ProviderTimeouts) { f.timeouts = t.withDefaults() }

// withTimeouts wraps p so each method gets its own deadline. Applied centrally
// in Fleet.provider(), so every call site — provisioner, scale-down, reconcile
// — is bounded without remembering to wrap. A shorter deadline already on the
// caller's context still wins; this is a ceiling, not a floor.
func withTimeouts(p compute.Provider, t ProviderTimeouts) compute.Provider {
	return &timeoutProvider{Provider: p, t: t.withDefaults()}
}

// timeoutProvider embeds Provider so Name/Classes (pure accessors, no I/O) pass
// straight through and new interface methods fail loudly at compile time rather
// than silently losing their timeout.
type timeoutProvider struct {
	compute.Provider
	t ProviderTimeouts
}

// call applies op's deadline and reports its latency and outcome. Timing here —
// inside the same wrapper that enforces the ceiling — is what makes the two
// numbers comparable: the histogram is measured against the exact budget the
// timeout will cut against.
func (p *timeoutProvider) call(ctx context.Context, op string, d time.Duration, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	start := time.Now()
	err := fn(ctx)
	recordProviderCall(ctx, p.Name(), op, time.Since(start), err)
	return err
}

func (p *timeoutProvider) Quote(ctx context.Context, req compute.Requirements) ([]compute.Offer, error) {
	var offers []compute.Offer
	err := p.call(ctx, "quote", p.t.Quote, func(ctx context.Context) (err error) {
		offers, err = p.Provider.Quote(ctx, req)
		return err
	})
	return offers, err
}

// Create's timeout is the one that can cost money: if the launch lands after we
// give up, the instance exists and Flint's row is marked failed. That leak is
// recovered by reconciliation — the instance carries the MachineID tag, and the
// zombie pass destroys instances Flint no longer owns.
func (p *timeoutProvider) Create(ctx context.Context, offer compute.Offer, b compute.Bootstrap) (compute.MachineRef, error) {
	var ref compute.MachineRef
	err := p.call(ctx, "create", p.t.Create, func(ctx context.Context) (err error) {
		ref, err = p.Provider.Create(ctx, offer, b)
		return err
	})
	return ref, err
}

func (p *timeoutProvider) Destroy(ctx context.Context, ref compute.MachineRef) error {
	return p.call(ctx, "destroy", p.t.Destroy, func(ctx context.Context) error {
		return p.Provider.Destroy(ctx, ref)
	})
}

func (p *timeoutProvider) List(ctx context.Context) ([]compute.MachineRef, error) {
	var refs []compute.MachineRef
	err := p.call(ctx, "list", p.t.List, func(ctx context.Context) (err error) {
		refs, err = p.Provider.List(ctx)
		return err
	})
	return refs, err
}
