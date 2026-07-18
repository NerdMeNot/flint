// Package computetest is the certification kit for compute providers: a
// conformance suite any implementation should pass, and an in-memory Fake
// elastic provider for fleet-manager tests. Third-party providers run
// RunConformance in their own test suites — passing it is what "works with
// Flint" means.
package computetest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/compute"
)

// RunConformance exercises the provider contract. mk builds a fresh provider
// per subtest. Providers without elastic capacity (Quote returns nothing) get
// the static branch: Create must be ErrUnsupported, Destroy idempotent-success,
// List ErrNotReconcilable. Elastic providers get the full lifecycle.
func RunConformance(t *testing.T, mk func(t *testing.T) compute.Provider) {
	t.Helper()
	ctx := context.Background()

	baseReq := compute.Requirements{
		CPUMillis: 2000, MemoryMB: 4096, Arch: "amd64", Capacity: compute.CapacityAny,
	}

	t.Run("Name is non-empty", func(t *testing.T) {
		require.NotEmpty(t, mk(t).Name())
	})

	t.Run("Quote", func(t *testing.T) {
		p := mk(t)
		offers, err := p.Quote(ctx, baseReq)
		require.NoError(t, err)
		if len(offers) == 0 {
			return // static-style provider: nothing to price
		}
		// An elastic provider must advertise the classes it actually quotes, so
		// the fleet's DegradeCapacity can trust Classes() (advertising a class you
		// never supply would let a degrade pick a dead class).
		classes := p.Classes()
		require.NotEmpty(t, classes, "an elastic provider must advertise its reliability classes")
		for _, o := range offers {
			assert.NotEmpty(t, o.Provider, "offer must carry the provider name")
			assert.NotEmpty(t, o.InstanceType)
			assert.GreaterOrEqual(t, o.CPUMillis, baseReq.CPUMillis, "offer must satisfy cpu")
			assert.GreaterOrEqual(t, o.MemoryMB, baseReq.MemoryMB, "offer must satisfy memory")
			assert.Equal(t, baseReq.Arch, o.Arch, "offer must match arch")
			assert.Greater(t, o.PricePerHourUSD, 0.0, "offer must be priced")
			assert.Contains(t, classes, o.Capacity, "every offer's class must be advertised by Classes()")
		}

		// Arch filter must hold: an arm64 request never yields amd64 offers.
		armReq := baseReq
		armReq.Arch = "arm64"
		armOffers, err := p.Quote(ctx, armReq)
		require.NoError(t, err)
		for _, o := range armOffers {
			assert.Equal(t, "arm64", o.Arch)
		}

		// Instance-type allow-list must hold.
		if len(offers) > 0 {
			narrowed := baseReq
			narrowed.InstanceTypes = []string{offers[0].InstanceType}
			narrowOffers, err := p.Quote(ctx, narrowed)
			require.NoError(t, err)
			for _, o := range narrowOffers {
				assert.Equal(t, offers[0].InstanceType, o.InstanceType)
			}
		}
	})

	t.Run("Create idempotent per MachineID, Destroy idempotent, List reconciles", func(t *testing.T) {
		p := mk(t)
		offers, err := p.Quote(ctx, baseReq)
		require.NoError(t, err)

		if len(offers) == 0 {
			// Static branch.
			_, err := p.Create(ctx, compute.Offer{}, compute.Bootstrap{MachineID: "m-1"})
			assert.ErrorIs(t, err, compute.ErrUnsupported, "static providers must reject Create")
			assert.NoError(t, p.Destroy(ctx, compute.MachineRef{Provider: p.Name(), ID: "unknown"}),
				"Destroy of an unknown ref must be success")
			_, err = p.List(ctx)
			assert.ErrorIs(t, err, compute.ErrNotReconcilable)
			return
		}

		bootstrap := compute.Bootstrap{
			ServerGRPCURL: "grpcs://flint.example:9443", ServerHTTPURL: "https://flint.example",
			MachineID: "machine-conformance-1", RegistrationToken: "tok", PoolName: "standard",
		}
		ref1, err := p.Create(ctx, offers[0], bootstrap)
		require.NoError(t, err)
		require.NotEmpty(t, ref1.ID)

		// Same MachineID again → same machine, no second instance.
		ref2, err := p.Create(ctx, offers[0], bootstrap)
		require.NoError(t, err)
		assert.Equal(t, ref1.ID, ref2.ID, "Create must be idempotent per bootstrap.MachineID")

		// List contains the created ref, and surfaces the MachineID it was
		// created for — reconciliation correlates on that tag so a still-being-
		// recorded machine isn't destroyed as a zombie. A provider that can't tag
		// instances leaves it empty, but then it can't be crash-safe under
		// concurrent reconcile.
		refs, err := p.List(ctx)
		require.NoError(t, err)
		assert.True(t, containsRef(refs, ref1.ID), "List must include created machines")
		for _, r := range refs {
			if r.ID == ref1.ID {
				assert.Equal(t, bootstrap.MachineID, r.MachineID,
					"List must surface the MachineID the instance was created for")
			}
		}

		// Destroy, twice (idempotent), and List drops it.
		require.NoError(t, p.Destroy(ctx, ref1))
		require.NoError(t, p.Destroy(ctx, ref1), "second Destroy must be success")
		require.NoError(t, p.Destroy(ctx, compute.MachineRef{Provider: p.Name(), ID: "never-existed"}),
			"Destroy of an unknown ref must be success")

		refs, err = p.List(ctx)
		require.NoError(t, err)
		assert.False(t, containsLiveRef(refs, ref1.ID), "List must not report destroyed machines as live")
	})

	t.Run("Quote respects MaxBootSeconds", func(t *testing.T) {
		p := mk(t)
		req := baseReq
		req.MaxBootSeconds = 1
		offers, err := p.Quote(ctx, req)
		require.NoError(t, err)
		for _, o := range offers {
			assert.LessOrEqual(t, o.ExpectedBootSeconds, 1)
		}
	})

	t.Run("expired context is respected", func(t *testing.T) {
		p := mk(t)
		cctx, cancel := context.WithDeadline(ctx, time.Now().Add(-time.Second))
		defer cancel()
		if _, err := p.Quote(cctx, baseReq); err != nil {
			// Providers MAY fail on a dead context; what they must not do is
			// return a non-context error class.
			assert.True(t,
				errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled),
				"Quote on a dead context should surface the context error, got: %v", err)
		}
	})
}

func containsRef(refs []compute.MachineRef, id string) bool {
	for _, r := range refs {
		if r.ID == id {
			return true
		}
	}
	return false
}

func containsLiveRef(refs []compute.MachineRef, id string) bool {
	for _, r := range refs {
		if r.ID == id && r.State != compute.RefTerminated {
			return true
		}
	}
	return false
}
