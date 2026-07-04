package localdev

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/compute"
)

// stubAgent writes a tiny executable that ignores its args and blocks, standing
// in for a real flint-agent so the lifecycle can be exercised without a control
// plane. Returns its path.
func stubAgent(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "flint-agent")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\nsleep 30\n"), 0o755))
	return path
}

func newLocal(t *testing.T, cfg Config) *Provider {
	t.Helper()
	if cfg.AgentBinary == "" {
		cfg.AgentBinary = stubAgent(t)
	}
	if cfg.DataRoot == "" {
		cfg.DataRoot = t.TempDir()
	}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	p, err := newProvider(context.Background(), "local-test", raw, nil)
	require.NoError(t, err)
	return p.(*Provider)
}

func TestQuote_PricesHostArchOnly(t *testing.T) {
	p := newLocal(t, Config{PricePerHourUSD: 0.25, ExpectedBootSeconds: 4, InstanceType: "dev.box"})

	offers, err := p.Quote(context.Background(), compute.Requirements{
		CPUMillis: 2000, MemoryMB: 4096, Arch: runtime.GOARCH, Capacity: compute.CapacityAny,
	})
	require.NoError(t, err)
	require.Len(t, offers, 1)
	o := offers[0]
	assert.Equal(t, "local-test", o.Provider)
	assert.Equal(t, "dev.box", o.InstanceType)
	assert.Equal(t, compute.CapacityOnDemand, o.Capacity)
	assert.Equal(t, 0.25, o.PricePerHourUSD)
	assert.Equal(t, 4, o.ExpectedBootSeconds)
	assert.Equal(t, runtime.GOARCH, o.Arch)
	assert.Equal(t, int64(2000), o.CPUMillis)
}

func TestQuote_DeclinesWhatItCannotDo(t *testing.T) {
	p := newLocal(t, Config{})
	ctx := context.Background()

	foreign := "amd64"
	if runtime.GOARCH == "amd64" {
		foreign = "arm64"
	}
	for name, req := range map[string]compute.Requirements{
		"foreign arch": {Arch: foreign, Capacity: compute.CapacityAny},
		"spot":         {Arch: runtime.GOARCH, Capacity: compute.CapacitySpot},
		"gpu":          {Arch: runtime.GOARCH, Capacity: compute.CapacityAny, GPU: &compute.GPU{Vendor: "nvidia", Count: 1}},
	} {
		t.Run(name, func(t *testing.T) {
			offers, err := p.Quote(ctx, req)
			require.NoError(t, err)
			assert.Empty(t, offers, "must not offer for %s", name)
		})
	}
}

func TestCreate_IdempotentAndReconciles(t *testing.T) {
	p := newLocal(t, Config{ServerGRPCURL: "localhost:1"}) // unreachable is fine; the stub ignores it
	ctx := context.Background()
	bs := compute.Bootstrap{MachineID: "m-1", RegistrationToken: "tok", ServerGRPCURL: "localhost:1"}

	ref, err := p.Create(ctx, compute.Offer{}, bs)
	require.NoError(t, err)
	assert.Equal(t, "m-1", ref.ID)
	assert.Equal(t, compute.RefRunning, ref.State)

	// Idempotent: a second Create for the same MachineID reuses the process.
	_, err = p.Create(ctx, compute.Offer{}, bs)
	require.NoError(t, err)
	p.mu.Lock()
	assert.Len(t, p.procs, 1, "idempotent Create must not spawn a second process")
	p.mu.Unlock()

	// List reports it alive.
	refs, err := p.List(ctx)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, "m-1", refs[0].ID)

	// Destroy stops it and is idempotent.
	require.NoError(t, p.Destroy(ctx, ref))
	require.NoError(t, p.Destroy(ctx, ref))
	refs, err = p.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, refs, "destroyed machine must leave the inventory")

	// Data dir is cleaned.
	_, statErr := os.Stat(filepath.Join(p.cfg.DataRoot, "m-1"))
	assert.True(t, os.IsNotExist(statErr), "Destroy must remove the machine's data dir")
}

func TestNewProvider_RejectsMissingBinary(t *testing.T) {
	raw, _ := json.Marshal(Config{AgentBinary: "/no/such/flint-agent"})
	_, err := newProvider(context.Background(), "x", raw, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
