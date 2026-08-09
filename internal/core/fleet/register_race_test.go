package fleet_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/fleet"
)

// registerRaceFixture creates a pool and a machine sitting in 'requested' with a
// live bootstrap token — the exact state a machine is in between
// provider.Create returning and the provisioner committing requested →
// provisioning.
func registerRaceFixture(t *testing.T, h *harness, poolName string) (machineID, token string) {
	t.Helper()
	ctx := context.Background()

	require.NoError(t, h.q.UpsertMachinePool(ctx, db.UpsertMachinePoolParams{
		Name: poolName, Provider: "fake-cloud", Arch: "amd64", Cpu: "2", Memory: "4Gi",
		CapacityType: "any", Objective: "cost", MinWarm: 0, MaxMachines: 3, IdleTtlSeconds: 60,
	}))
	pool, err := h.q.GetMachinePool(ctx, poolName)
	require.NoError(t, err)

	token, hash, err := fleet.MintToken()
	require.NoError(t, err)

	machineID, err = h.q.InsertMachine(ctx, db.InsertMachineParams{
		PoolID: pool.ID, Provider: "fake-cloud", CpuMillis: 2000, MemoryMb: 4096,
		Arch: "amd64", BootstrapTokenHash: &hash,
	})
	require.NoError(t, err)
	return machineID, token
}

// A machine that registers while the provisioner is still committing
// requested → provisioning must register successfully, not fail.
//
// Register transitions the machine FROM the status it read, and that transition
// is guarded by `WHERE status = @from_status`. When the read wasn't locked, this
// interleaving made the guard miss: Register read 'requested', the provisioner
// committed 'provisioning', and Register's update matched zero rows and
// surfaced as a lost-race error to a machine that had booted perfectly well.
//
// The interleaving is forced rather than raced, so this fails deterministically
// if the row lock is ever dropped.
func TestRegister_SurvivesConcurrentProvisioningTransition(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	machineID, token := registerRaceFixture(t, h, "register-race-pool")

	// Hold the machine row in an uncommitted requested → provisioning update,
	// standing in for the provisioner's in-flight transaction.
	holdTx, err := h.pool.Begin(ctx)
	require.NoError(t, err)
	tag, err := holdTx.Exec(ctx,
		`UPDATE machines SET status = 'provisioning' WHERE id = $1 AND status = 'requested'`,
		machineID)
	require.NoError(t, err)
	require.EqualValues(t, 1, tag.RowsAffected(), "fixture must start in 'requested'")

	// Register concurrently: it blocks on the locked row rather than reading a
	// status that is about to be stale.
	type result struct {
		reg fleet.RegisteredMachine
		err error
	}
	done := make(chan result, 1)
	go func() {
		reg, err := h.fleet.Register(context.Background(), fleet.Registration{
			Token: token, Hostname: "racer", Arch: "amd64", OS: "linux",
			CPUMillis: 2000, MemoryMB: 4096, DiskGB: 40,
		})
		done <- result{reg, err}
	}()

	select {
	case r := <-done:
		t.Fatalf("Register returned while the row was still locked (err=%v) — "+
			"it read the status without taking the lock", r.err)
	case <-time.After(250 * time.Millisecond):
		// Expected: blocked on the row lock.
	}

	require.NoError(t, holdTx.Commit(ctx))

	select {
	case r := <-done:
		require.NoError(t, r.err, "a machine that booted fine must not fail registration")
		assert.Equal(t, machineID, r.reg.MachineID)
		assert.NotEmpty(t, r.reg.MachineToken)
	case <-time.After(10 * time.Second):
		t.Fatal("Register never completed after the provisioner's transaction committed")
	}

	m, err := h.q.GetMachine(ctx, machineID)
	require.NoError(t, err)
	assert.Equal(t, "idle", m.Status, "a registered machine is idle and schedulable")
	assert.Nil(t, m.BootstrapTokenHash, "the single-use bootstrap token must be consumed")
}

// The mirror ordering: registration wins the lock first, and the provisioner's
// transition is the one that finds the row already moved. That path is expected
// and handled (the provisioner records the provider ref and leaves the machine
// idle), so the machine must still end up idle and usable.
func TestRegister_WinsRaceAndLeavesMachineIdle(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	machineID, token := registerRaceFixture(t, h, "register-race-pool-2")

	_, err := h.fleet.Register(ctx, fleet.Registration{
		Token: token, Hostname: "fast", Arch: "amd64", OS: "linux",
		CPUMillis: 2000, MemoryMB: 4096, DiskGB: 40,
	})
	require.NoError(t, err)

	// The provisioner's late transition finds the row already at 'idle'.
	tag, err := h.pool.Exec(ctx,
		`UPDATE machines SET status = 'provisioning' WHERE id = $1 AND status = 'requested'`,
		machineID)
	require.NoError(t, err)
	assert.EqualValues(t, 0, tag.RowsAffected(),
		"the guard must refuse to walk a live machine backwards to provisioning")

	m, err := h.q.GetMachine(ctx, machineID)
	require.NoError(t, err)
	assert.Equal(t, "idle", m.Status)
}
