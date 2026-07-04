package fleet_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/dbkit"
	"github.com/NerdMeNot/flint/internal/testutil/pgtest"
)

// TestPoolInsightQueries validates the 7d insight aggregations against real
// Postgres (percentile_cont with FILTER is beyond what mocks exercise):
// spend clipped to the window, boot p50, warm-hit classification, and the
// warm/cold queue split.
func TestPoolInsightQueries(t *testing.T) {
	ctx := context.Background()
	dsn := pgtest.DSN(t)
	require.NoError(t, dbkit.RunMigrations(dsn, dbkit.Migrations, "migrations"))
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	q := db.New(pool)

	// Unique pool per run: CI reuses one database across `go test` and
	// `go test -race`, so a fixed pool name would accumulate machines and
	// double every aggregate.
	poolName := "insights-" + uuid.NewString()[:8]
	require.NoError(t, q.UpsertMachinePool(ctx, db.UpsertMachinePoolParams{
		Name: poolName, Provider: "static", Arch: "amd64", Cpu: "4", Memory: "8Gi",
		CapacityType: "on_demand", Objective: "balanced", MaxMachines: 10, IdleTtlSeconds: 900,
	}))
	poolRow, err := q.GetMachinePool(ctx, poolName)
	require.NoError(t, err)

	// Machine A: warm for 2h in-window at $0.50/hr, booted in 60s.
	// Machine B: spot, lost (an interruption), 1h of life, booted in 120s.
	warmID, spotID := uuid.NewString(), uuid.NewString()
	_, err = pool.Exec(ctx, `
		INSERT INTO machines (id, pool_id, status, provider, capacity_type, price_per_hour_usd,
		                      cpu_millis, memory_mb, arch, requested_at, registered_at, terminated_at, updated_at)
		VALUES
		  ($1, $2, 'idle', 'static', 'on_demand', 0.50, 4000, 8192, 'amd64',
		   now() - interval '2 hours 1 minute', now() - interval '2 hours', NULL, now()),
		  ($3, $2, 'lost', 'static', 'spot', 0.20, 4000, 8192, 'amd64',
		   now() - interval '3 hours 2 minutes', now() - interval '3 hours', now() - interval '2 hours', now())
	`, warmID, poolRow.ID, spotID)
	require.NoError(t, err)

	// Warm hit: machine registered before the assignment arrived (5s queue).
	// Cold hit: assignment predates registration (65s queue = boot wait).
	stepA, stepB := uuid.NewString(), uuid.NewString()
	wfID, runID := uuid.NewString(), uuid.NewString()
	_, err = pool.Exec(ctx, `
		INSERT INTO step_assignments (id, step_id, workflow_id, run_id, step_name, attempt, pool_id,
		                              machine_id, status, cpu_millis, memory_mb, disk_gb, payload,
		                              created_at, started_at, finished_at)
		VALUES
		  (gen_random_uuid(), $1, $3, $4, 'warm-step', 0, $5, $6, 'succeeded', 1000, 1024, 0, '{}',
		   now() - interval '90 minutes', now() - interval '90 minutes' + interval '5 seconds', now() - interval '80 minutes'),
		  (gen_random_uuid(), $2, $3, $4, 'cold-step', 0, $5, $7, 'succeeded', 1000, 1024, 0, '{}',
		   now() - interval '3 hours 2 minutes', now() - interval '3 hours 2 minutes' + interval '65 seconds', now() - interval '3 hours')
	`, stepA, stepB, wfID, runID, poolRow.ID, warmID, spotID)
	require.NoError(t, err)

	machines, err := q.PoolInsightMachines(ctx, poolRow.ID)
	require.NoError(t, err)
	// ~2h + ~1h of in-window life.
	assert.InDelta(t, 3.05, machines.MachineHours, 0.15, "machine hours")
	// 2.02h × $0.50 + 1.03h × $0.20 ≈ $1.21
	assert.InDelta(t, 1.21, machines.SpendUsd, 0.05, "spend")
	assert.EqualValues(t, 2, machines.Boots)
	// Boot times 60s and 120s → p50 = 90s.
	assert.InDelta(t, 90, machines.BootP50Secs, 2, "boot p50")
	assert.EqualValues(t, 1, machines.Interruptions, "spot lost counts as an interruption")
	assert.InDelta(t, 0.35, machines.PriceP50Usd, 0.01, "price p50")

	assigns, err := q.PoolInsightAssignments(ctx, poolRow.ID)
	require.NoError(t, err)
	assert.EqualValues(t, 2, assigns.Total)
	assert.EqualValues(t, 1, assigns.WarmHits)
	assert.InDelta(t, 5, assigns.WarmQueueP50Secs, 1, "warm queue p50")
	assert.InDelta(t, 65, assigns.ColdQueueP50Secs, 1, "cold queue p50")
	assert.InDelta(t, 35, assigns.QueueP50Secs, 2, "overall queue p50 = midpoint of 5 and 65")
}
