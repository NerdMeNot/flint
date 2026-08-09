package fleet

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/compute"
)

// blockingProvider hangs on every call until its context is cancelled — the
// failure mode the timeouts exist for (a cloud API that accepts the connection
// and then never answers).
type blockingProvider struct{ started chan struct{} }

func (p *blockingProvider) Name() string                    { return "blocking" }
func (p *blockingProvider) Classes() []compute.CapacityType { return nil }

func (p *blockingProvider) block(ctx context.Context) error {
	select {
	case p.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return ctx.Err()
}

func (p *blockingProvider) Quote(ctx context.Context, _ compute.Requirements) ([]compute.Offer, error) {
	return nil, p.block(ctx)
}

func (p *blockingProvider) Create(ctx context.Context, _ compute.Offer, _ compute.Bootstrap) (compute.MachineRef, error) {
	return compute.MachineRef{}, p.block(ctx)
}

func (p *blockingProvider) Destroy(ctx context.Context, _ compute.MachineRef) error {
	return p.block(ctx)
}

func (p *blockingProvider) List(ctx context.Context) ([]compute.MachineRef, error) {
	return nil, p.block(ctx)
}

// A hung provider must not be able to park a fleet loop forever. Every method
// is checked, because one unwrapped method is enough to stall the whole loop.
func TestProviderTimeouts_HangingCallsAllReturn(t *testing.T) {
	short := ProviderTimeouts{
		Quote:   50 * time.Millisecond,
		Create:  50 * time.Millisecond,
		Destroy: 50 * time.Millisecond,
		List:    50 * time.Millisecond,
	}
	p := withTimeouts(&blockingProvider{started: make(chan struct{}, 1)}, short)

	calls := map[string]func() error{
		"quote": func() error { _, err := p.Quote(context.Background(), compute.Requirements{}); return err },
		"create": func() error {
			_, err := p.Create(context.Background(), compute.Offer{}, compute.Bootstrap{})
			return err
		},
		"destroy": func() error { return p.Destroy(context.Background(), compute.MachineRef{}) },
		"list":    func() error { _, err := p.List(context.Background()); return err },
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			done := make(chan error, 1)
			start := time.Now()
			go func() { done <- call() }()

			select {
			case err := <-done:
				assert.ErrorIs(t, err, context.DeadlineExceeded,
					"a hung provider call must surface as a deadline, not a nil error")
				assert.Less(t, time.Since(start), 2*time.Second,
					"the call must return on its own deadline")
			case <-time.After(2 * time.Second):
				t.Fatal("provider call was not bounded — the fleet loop would stall here")
			}
		})
	}
}

// The zero value must already be bounded: a Fleet nobody configured is the
// common case, and "forgot to set timeouts" must not mean "unbounded".
func TestProviderTimeouts_ZeroValueUsesDefaults(t *testing.T) {
	got := ProviderTimeouts{}.withDefaults()
	assert.Equal(t, DefaultProviderTimeouts(), got)

	// A partial override keeps its own value and defaults the rest.
	partial := ProviderTimeouts{Quote: time.Second}.withDefaults()
	assert.Equal(t, time.Second, partial.Quote)
	assert.Equal(t, DefaultProviderTimeouts().Create, partial.Create)
	assert.Equal(t, DefaultProviderTimeouts().List, partial.List)
}

// passthroughProvider records whether the wrapper forwards results and errors
// untouched when nothing times out.
type passthroughProvider struct{ err error }

func (p *passthroughProvider) Name() string { return "passthrough" }
func (p *passthroughProvider) Classes() []compute.CapacityType {
	return []compute.CapacityType{compute.CapacityOnDemand}
}
func (p *passthroughProvider) Quote(context.Context, compute.Requirements) ([]compute.Offer, error) {
	return []compute.Offer{{InstanceType: "m5.large"}}, p.err
}
func (p *passthroughProvider) Create(context.Context, compute.Offer, compute.Bootstrap) (compute.MachineRef, error) {
	return compute.MachineRef{ID: "i-123"}, p.err
}
func (p *passthroughProvider) Destroy(context.Context, compute.MachineRef) error { return p.err }
func (p *passthroughProvider) List(context.Context) ([]compute.MachineRef, error) {
	return []compute.MachineRef{{ID: "i-123"}}, p.err
}

// The wrapper is a decorator, not a filter: fast calls must be indistinguishable
// from unwrapped ones, including the provider's own errors and Name/Classes.
func TestProviderTimeouts_PassesThroughWhenFast(t *testing.T) {
	p := withTimeouts(&passthroughProvider{}, DefaultProviderTimeouts())

	assert.Equal(t, "passthrough", p.Name())
	assert.Equal(t, []compute.CapacityType{compute.CapacityOnDemand}, p.Classes())

	offers, err := p.Quote(context.Background(), compute.Requirements{})
	require.NoError(t, err)
	require.Len(t, offers, 1)
	assert.Equal(t, "m5.large", offers[0].InstanceType)

	ref, err := p.Create(context.Background(), compute.Offer{}, compute.Bootstrap{})
	require.NoError(t, err)
	assert.Equal(t, "i-123", ref.ID)

	sentinel := errors.New("provider refused")
	failing := withTimeouts(&passthroughProvider{err: sentinel}, DefaultProviderTimeouts())
	assert.ErrorIs(t, failing.Destroy(context.Background(), compute.MachineRef{}), sentinel,
		"a provider's own error must reach the caller unchanged")
}
