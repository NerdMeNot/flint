// Package staticpool implements the compute provider for bring-your-own
// machines. It has no elastic capacity: machines enter a pool by running
// flint-agent with the pool's join token, and leave when the operator stops
// the agent. The provider therefore quotes nothing, creates nothing, and has
// no inventory to reconcile — the DB (fed by registrations and heartbeats) is
// the only source of truth.
package staticpool

import (
	"context"
	"encoding/json"

	"github.com/NerdMeNot/flint/pkg/compute"
)

func init() {
	compute.Register("static", func(_ context.Context, name string, _ json.RawMessage, _ []byte) (compute.Provider, error) {
		return &Provider{name: name}, nil
	})
}

// Provider is the no-op elastic surface of a static machine pool.
type Provider struct {
	name string
}

func (p *Provider) Name() string { return p.name }

// Classes: static pools have no elastic capacity to advertise.
func (p *Provider) Classes() []compute.CapacityType { return nil }

// Quote returns no offers: static pools cannot mint capacity on demand.
func (p *Provider) Quote(context.Context, compute.Requirements) ([]compute.Offer, error) {
	return nil, nil
}

// Create is unsupported: machines join via the pool join token.
func (p *Provider) Create(context.Context, compute.Offer, compute.Bootstrap) (compute.MachineRef, error) {
	return compute.MachineRef{}, compute.ErrUnsupported
}

// Destroy is a no-op success: Flint cannot terminate hardware it did not
// create; draining the machine (stopping work) is the fleet's job, and the
// operator owns the power button.
func (p *Provider) Destroy(context.Context, compute.MachineRef) error {
	return nil
}

// List has no provider-side inventory to reconcile against.
func (p *Provider) List(context.Context) ([]compute.MachineRef, error) {
	return nil, compute.ErrNotReconcilable
}
