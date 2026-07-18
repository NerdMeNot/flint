package fleet_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/fleet"
	"github.com/NerdMeNot/flint/pkg/compute"
	"github.com/NerdMeNot/flint/pkg/compute/computetest"
	agentv1 "github.com/NerdMeNot/flint/protogen/agent/v1"
)

// elasticHarness extends the base harness with a Fake elastic provider whose
// OnCreate hook boots a "machine" by registering through the real gRPC path
// with the bootstrap token — the full elastic lifecycle with no cloud.
func elasticHarness(t *testing.T, poolName string) (*harness, *computetest.Fake) {
	t.Helper()
	h := newHarness(t)
	ctx := context.Background()

	fake := computetest.NewFake("fake-cloud")
	h.fleet.SetProviderResolver(func(_ context.Context, name string) (compute.Provider, error) {
		return fake, nil
	})
	h.fleet.SetBootstrapEndpoints(fleet.BootstrapEndpoints{
		ServerGRPCURL: "bufconn", ServerHTTPURL: "http://flint.test:8080",
	})

	// Booted "instances" register like real agents would from cloud-init.
	fake.OnCreate = func(m computetest.FakeMachine) {
		go func() {
			// Fire-and-forget, like cloud-init. This goroutine can outlive the
			// test (the provider Create returns before registration completes), so
			// it must never touch t.* — logging after the test finishes panics the
			// whole package ("Log in goroutine after Test completed"), which showed
			// up as an intermittent suite FAIL. A registration that genuinely fails
			// surfaces deterministically as a waitForCond timeout in the test body.
			_, _ = h.client.RegisterMachine(context.Background(), agentRegisterReq(m))
		}()
	}

	require.NoError(t, h.q.UpsertMachinePool(ctx, db.UpsertMachinePoolParams{
		Name: poolName, Provider: "fake-cloud", Arch: "amd64", Cpu: "2", Memory: "4Gi",
		CapacityType: "any", Objective: "cost", MinWarm: 0, MaxMachines: 3, IdleTtlSeconds: 1,
	}))
	// Isolate from earlier tests in the shared package DB: the provisioner
	// iterates every ready pool.
	_, err := h.pool.Exec(ctx, `UPDATE machine_pools SET ready = false WHERE name != $1`, poolName)
	require.NoError(t, err)
	_, err = h.pool.Exec(ctx, `UPDATE step_assignments SET status = 'cancelled' WHERE status = 'pending'`)
	require.NoError(t, err)
	return h, fake
}

func TestElastic_ProvisionOnDemandAndScaleDown(t *testing.T) {
	h, fake := elasticHarness(t, "elastic-1")
	ctx := context.Background()
	poolID := poolIDByName(t, h, "elastic-1")

	// Pending demand with no machines → the provisioner boots one.
	h.insertPendingAssignment(t, poolID)
	booted, err := h.fleet.Provision(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, booted)

	// The fake's OnCreate registered the machine: provisioning → idle.
	waitForCond(t, 5*time.Second, func() bool {
		return countMachines(t, h, poolID, "idle") == 1
	}, "booted machine should register and go idle")

	// The provision decision is ledgered with a chosen offer and its outcome.
	decisions, err := h.q.ListFleetDecisions(ctx, db.ListFleetDecisionsParams{
		PoolID: &poolID, Limit: 10,
	})
	require.NoError(t, err)
	require.NotEmpty(t, decisions)
	prov := decisions[len(decisions)-1]
	assert.Equal(t, "provision", prov.DecisionType)
	assert.NotEmpty(t, prov.Chosen, "chosen offer must be recorded")
	require.NotNil(t, prov.Outcome)
	assert.Equal(t, "boot_ok", *prov.Outcome, "registration backfills the outcome")

	// Objective=cost with capacity=any: the cheapest (spot) offer wins.
	machines := fake.Machines()
	require.Len(t, machines, 1)
	assert.Equal(t, compute.CapacitySpot, machines[0].Offer.Capacity)

	// The scheduler binds the pending work to the new machine.
	if _, err := h.fleet.SchedulePending(ctx); err == nil {
		waitForCond(t, 3*time.Second, func() bool {
			var n int
			_ = h.pool.QueryRow(ctx,
				`SELECT count(*) FROM step_assignments WHERE pool_id = $1 AND status = 'assigned'`, poolID).Scan(&n)
			return n == 1
		}, "pending assignment should bind to the booted machine")
	}

	// Finish the work (raw SQL stands in for the agent's completion path,
	// which would drive busy → idle), then age the idle timestamp so the
	// 1-second TTL reaps the machine above minWarm=0.
	_, err = h.pool.Exec(ctx, `UPDATE step_assignments SET status = 'succeeded', finished_at = now() WHERE pool_id = $1`, poolID)
	require.NoError(t, err)
	_, err = h.pool.Exec(ctx,
		`UPDATE machines SET status = 'idle', idle_since = now() - interval '1 hour' WHERE pool_id = $1`, poolID)
	require.NoError(t, err)

	reaped, err := h.fleet.ScaleDown(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, reaped)
	assert.Equal(t, 1, countMachines(t, h, poolID, "terminated"))

	// The provider was actually asked to destroy the instance.
	refs, err := fake.List(ctx)
	require.NoError(t, err)
	require.Len(t, refs, 1)
	assert.Equal(t, compute.RefTerminated, refs[0].State)
}

func TestElastic_MinWarmFloorHolds(t *testing.T) {
	h, _ := elasticHarness(t, "warm-pool")
	ctx := context.Background()
	poolID := poolIDByName(t, h, "warm-pool")
	_, err := h.pool.Exec(ctx, `UPDATE machine_pools SET min_warm = 1 WHERE id = $1`, poolID)
	require.NoError(t, err)

	// No demand at all: minWarm=1 still boots one machine.
	booted, err := h.fleet.Provision(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, booted)
	waitForCond(t, 5*time.Second, func() bool {
		return countMachines(t, h, poolID, "idle") == 1
	}, "warm machine should register")

	// Idle far past TTL — but the warm floor protects it.
	_, err = h.pool.Exec(ctx, `UPDATE machines SET idle_since = now() - interval '1 hour' WHERE pool_id = $1`, poolID)
	require.NoError(t, err)
	reaped, err := h.fleet.ScaleDown(ctx)
	require.NoError(t, err)
	assert.Zero(t, reaped, "minWarm machines are the user's declared speed choice")

	// And provisioning again is a no-op (floor already met).
	booted, err = h.fleet.Provision(ctx)
	require.NoError(t, err)
	assert.Zero(t, booted)
}

func TestElastic_SpotKillReconcile(t *testing.T) {
	h, fake := elasticHarness(t, "spot-pool")
	ctx := context.Background()
	poolID := poolIDByName(t, h, "spot-pool")

	h.insertPendingAssignment(t, poolID)
	_, err := h.fleet.Provision(ctx)
	require.NoError(t, err)
	waitForCond(t, 5*time.Second, func() bool {
		return countMachines(t, h, poolID, "idle") == 1
	}, "machine should register")

	// Bind work to it so the kill has a victim.
	_, err = h.fleet.SchedulePending(ctx)
	require.NoError(t, err)

	// The cloud reclaims the spot instance out from under us.
	machines := fake.Machines()
	require.Len(t, machines, 1)
	require.True(t, fake.KillMachine(machines[0].Ref.ID))

	// Reconciliation notices provider truth: machine lost, assignment failed.
	require.NoError(t, h.fleet.Reconcile(ctx))

	var machineStatus string
	require.NoError(t, h.pool.QueryRow(ctx,
		`SELECT status FROM machines WHERE pool_id = $1`, poolID).Scan(&machineStatus))
	assert.Equal(t, "lost", machineStatus)

	var assignmentStatus string
	require.NoError(t, h.pool.QueryRow(ctx,
		`SELECT status FROM step_assignments WHERE pool_id = $1`, poolID).Scan(&assignmentStatus))
	assert.Equal(t, "lost", assignmentStatus, "spot kill fails the assignment through the engine seam")

	// A second reconcile pass finishes the lifecycle (destroy retry → terminated).
	require.NoError(t, h.fleet.Reconcile(ctx))
	require.NoError(t, h.pool.QueryRow(ctx,
		`SELECT status FROM machines WHERE pool_id = $1`, poolID).Scan(&machineStatus))
	assert.Equal(t, "terminated", machineStatus)
}

func TestElastic_BootTimeoutFailsMachine(t *testing.T) {
	h, fake := elasticHarness(t, "timeout-pool")
	ctx := context.Background()
	poolID := poolIDByName(t, h, "timeout-pool")

	// Machines that never register: drop the OnCreate hook.
	fake.OnCreate = nil

	h.insertPendingAssignment(t, poolID)
	_, err := h.fleet.Provision(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, countMachines(t, h, poolID, "provisioning"))

	// Force the boot deadline into the past; the sweep fails the machine and
	// backfills the ledger.
	_, err = h.pool.Exec(ctx, `UPDATE machines SET boot_deadline_at = now() - interval '1 minute' WHERE pool_id = $1`, poolID)
	require.NoError(t, err)
	n, err := h.fleet.ExpireBootDeadlines(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	assert.Equal(t, 1, countMachines(t, h, poolID, "failed"))

	decisions, err := h.q.ListFleetDecisions(ctx, db.ListFleetDecisionsParams{PoolID: &poolID, Limit: 10})
	require.NoError(t, err)
	found := false
	for _, d := range decisions {
		if d.DecisionType == "provision" && d.Outcome != nil && *d.Outcome == "boot_timeout" {
			found = true
		}
	}
	assert.True(t, found, "boot timeout should backfill the provision decision")
}

// ── helpers ──────────────────────────────────────────────────

// agentRegisterReq builds a registration request from the fake's bootstrap.
func agentRegisterReq(m computetest.FakeMachine) *agentv1.RegisterMachineRequest {
	return &agentv1.RegisterMachineRequest{
		RegistrationToken: m.Bootstrap.RegistrationToken,
		MachineId:         m.Bootstrap.MachineID,
		Hostname:          "fake-" + m.Ref.ID,
		Arch:              m.Offer.Arch,
		Os:                "linux",
		CpuMillis:         m.Offer.CPUMillis,
		MemoryMb:          m.Offer.MemoryMB,
		DiskGb:            m.Offer.DiskGB,
	}
}

func poolIDByName(t *testing.T, h *harness, name string) string {
	t.Helper()
	row, err := h.q.GetMachinePool(context.Background(), name)
	require.NoError(t, err)
	return row.ID
}

func countMachines(t *testing.T, h *harness, poolID, status string) int {
	t.Helper()
	var n int
	require.NoError(t, h.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM machines WHERE pool_id = $1 AND status = $2`, poolID, status).Scan(&n))
	return n
}

// insertPendingAssignment plants unschedulable demand in the pool.
func (h *harness) insertPendingAssignment(t *testing.T, poolID string) string {
	t.Helper()
	var id string
	require.NoError(t, h.pool.QueryRow(context.Background(), `
		INSERT INTO step_assignments (step_id, workflow_id, run_id, step_name, attempt,
			pool_id, status, cpu_millis, memory_mb, payload)
		VALUES (gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), 'pending-step', 0,
			$1, 'pending', 1000, 1024, '{}')
		RETURNING id`, poolID).Scan(&id))
	return id
}

func waitForCond(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out: %s", msg)
}
