package fleet_test

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/test/bufconn"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/dbkit"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/core/fleet"
	"github.com/NerdMeNot/flint/internal/platform/agentgrpc"
	"github.com/NerdMeNot/flint/internal/testutil/pgtest"
	"github.com/NerdMeNot/flint/pkg/logsink"
	agentv1 "github.com/NerdMeNot/flint/protogen/agent/v1"
)

func TestMain(m *testing.M) {
	os.Exit(pgtest.Main(m))
}

// harness wires a real Postgres, real engine, real fleet, and the AgentService
// over bufconn — the full server side of the agent protocol with no network.
type harness struct {
	pool   *pgxpool.Pool
	q      *db.Queries
	eng    *engine.PgEngine
	fleet  *fleet.Fleet
	client agentv1.AgentServiceClient
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	dsn := pgtest.DSN(t)

	require.NoError(t, dbkit.RunMigrations(dsn, dbkit.Migrations, "migrations"))
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	eng := engine.New(pool, []byte("test-signing-key-0123456789abcdef"))
	t.Cleanup(func() { eng.Close() })

	fl := fleet.New(pool, eng)
	srv := agentgrpc.New(fl, eng, &logsink.FilesystemSink{BaseDir: t.TempDir()}, nil, agentgrpc.Config{
		ServerHTTPURL: "http://flint.test:8080",
	})

	lis := bufconn.Listen(1 << 20)
	go func() { _ = srv.GRPCServer().Serve(lis) }()
	t.Cleanup(srv.GRPCServer().Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	return &harness{
		pool: pool, q: db.New(pool), eng: eng, fleet: fl,
		client: agentv1.NewAgentServiceClient(conn),
	}
}

// seedPool creates a static machine pool and mints its join token.
func (h *harness) seedPool(t *testing.T, name string) (joinToken string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, h.q.UpsertMachinePool(ctx, db.UpsertMachinePoolParams{
		Name: name, Provider: "static", Arch: "amd64", Cpu: "4", Memory: "8Gi",
		CapacityType: "on_demand", Objective: "balanced", MaxMachines: 10, IdleTtlSeconds: 900,
	}))
	token, hash, err := fleet.MintToken()
	require.NoError(t, err)
	require.NoError(t, h.q.SetPoolJoinTokenHash(ctx, db.SetPoolJoinTokenHashParams{
		Name: name, JoinTokenHash: &hash,
	}))
	return token
}

// register joins a static machine and returns its id + authed context factory.
func (h *harness) register(t *testing.T, joinToken string) (machineID string, authed func(context.Context) context.Context) {
	t.Helper()
	resp, err := h.client.RegisterMachine(context.Background(), &agentv1.RegisterMachineRequest{
		RegistrationToken: joinToken,
		Hostname:          "test-box", Arch: "amd64", Os: "linux",
		CpuMillis: 8000, MemoryMb: 16384, DiskGb: 100,
		AgentVersion: "test",
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.GetMachineId())
	require.NotEmpty(t, resp.GetMachineToken())
	token := resp.GetMachineToken()
	return resp.GetMachineId(), func(ctx context.Context) context.Context {
		return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+token)
	}
}

func TestRegisterHeartbeatLeaseLifecycle(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	joinToken := h.seedPool(t, "static-itest")

	machineID, authed := h.register(t, joinToken)

	// Registered machine is idle with capacity recorded.
	m, err := h.q.GetMachine(ctx, machineID)
	require.NoError(t, err)
	assert.Equal(t, "idle", m.Status)
	assert.Equal(t, int64(8000), m.CpuMillis)
	assert.Nil(t, m.BootstrapTokenHash)

	// A bad token is rejected.
	_, err = h.client.RegisterMachine(ctx, &agentv1.RegisterMachineRequest{RegistrationToken: "nope"})
	require.Error(t, err)

	// Unauthenticated calls are rejected; authenticated heartbeat renews the lease.
	_, err = h.client.Heartbeat(ctx, &agentv1.HeartbeatRequest{MachineId: machineID})
	require.Error(t, err, "heartbeat without bearer token must fail")

	hb, err := h.client.Heartbeat(authed(ctx), &agentv1.HeartbeatRequest{MachineId: machineID})
	require.NoError(t, err)
	assert.Equal(t, "continue", hb.GetAction())

	// ClaimStep long-poll with nothing assigned returns unassigned.
	claim, err := h.client.ClaimStep(authed(ctx), &agentv1.ClaimStepRequest{MachineId: machineID, WaitSeconds: 1})
	require.NoError(t, err)
	assert.False(t, claim.GetAssigned())

	// Lease expiry → lost, and live assignments on the machine fail.
	assignmentID := h.insertAssignment(t, machineID, "running")
	_, err = h.pool.Exec(ctx, `UPDATE machines SET heartbeat_expires_at = now() - interval '1 minute' WHERE id = $1`, machineID)
	require.NoError(t, err)
	lost, err := h.fleet.ExpireHeartbeatLeases(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, lost)

	m, err = h.q.GetMachine(ctx, machineID)
	require.NoError(t, err)
	assert.Equal(t, "lost", m.Status)
	a, err := h.q.GetAssignment(ctx, assignmentID)
	require.NoError(t, err)
	assert.Equal(t, "lost", a.Status)

	// The machine's next heartbeat brings it back (static box reappearing).
	hb, err = h.client.Heartbeat(authed(ctx), &agentv1.HeartbeatRequest{MachineId: machineID})
	require.NoError(t, err)
	assert.Equal(t, "continue", hb.GetAction())
	m, err = h.q.GetMachine(ctx, machineID)
	require.NoError(t, err)
	assert.Equal(t, "idle", m.Status)

	// Machine events recorded the whole story.
	events, err := h.q.ListMachineEvents(ctx, db.ListMachineEventsParams{MachineID: machineID, Limit: 20})
	require.NoError(t, err)
	kinds := map[string]bool{}
	for _, e := range events {
		kinds[e.EventType] = true
	}
	assert.True(t, kinds["registered"], "registered event")
	assert.True(t, kinds["heartbeat_expired"], "heartbeat_expired event")
	assert.True(t, kinds["reappeared"], "reappeared event")
}

func TestClaimDeliversAssignedWork(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	joinToken := h.seedPool(t, "static-claim")
	machineID, authed := h.register(t, joinToken)

	assignmentID := h.insertAssignment(t, machineID, "assigned")

	claim, err := h.client.ClaimStep(authed(ctx), &agentv1.ClaimStepRequest{MachineId: machineID, WaitSeconds: 5})
	require.NoError(t, err)
	require.True(t, claim.GetAssigned())
	got := claim.GetAssignment()
	assert.Equal(t, assignmentID, got.GetAssignmentId())
	assert.Equal(t, "build", got.GetStepName())
	assert.Equal(t, "echo hello", jsonField(t, got.GetPayload().GetStepDefJson(), "run"))
	assert.Equal(t, "task-token-test", got.GetPayload().GetTaskToken())

	// Claiming moved it to running.
	a, err := h.q.GetAssignment(ctx, assignmentID)
	require.NoError(t, err)
	assert.Equal(t, "running", a.Status)

	// A second claim finds nothing.
	claim2, err := h.client.ClaimStep(authed(ctx), &agentv1.ClaimStepRequest{MachineId: machineID, WaitSeconds: 1})
	require.NoError(t, err)
	assert.False(t, claim2.GetAssigned())
}

func TestSelfDrainViaHeartbeat(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	joinToken := h.seedPool(t, "static-drain")
	machineID, authed := h.register(t, joinToken)

	hb, err := h.client.Heartbeat(authed(ctx), &agentv1.HeartbeatRequest{
		MachineId: machineID, DrainingRequested: true, DrainReason: "spot_interruption",
	})
	require.NoError(t, err)
	assert.Equal(t, "drain", hb.GetAction())

	m, err := h.q.GetMachine(ctx, machineID)
	require.NoError(t, err)
	assert.Equal(t, "draining", m.Status)
	require.NotNil(t, m.DrainReason)
	assert.Equal(t, "spot_interruption", *m.DrainReason)
}

// insertAssignment plants an assignment bound to the machine, bypassing the
// scheduler (which lands in the next phase).
func (h *harness) insertAssignment(t *testing.T, machineID, status string) string {
	t.Helper()
	ctx := context.Background()
	stepDef, _ := json.Marshal(map[string]any{"name": "build", "run": "echo hello"})
	payload, _ := json.Marshal(map[string]any{
		"step_def_json": stepDef,
		"task_token":    "task-token-test",
		"run_id":        uuid.NewString(),
		"step_name":     "build",
	})
	pid := h.poolIDFor(t, machineID)
	id := uuid.NewString()
	_, err := h.pool.Exec(ctx, `
		INSERT INTO step_assignments (id, step_id, workflow_id, run_id, step_name, attempt,
			pool_id, machine_id, status, cpu_millis, memory_mb, payload, assigned_at, started_at)
		VALUES ($1, $2, $3, $4, 'build', 0, $5, $6, $7, 1000, 1024, $8, now(),
			CASE WHEN $7 = 'running' THEN now() ELSE NULL END)`,
		id, uuid.NewString(), uuid.NewString(), uuid.NewString(), pid, machineID, status, payload)
	require.NoError(t, err)
	return id
}

func (h *harness) poolIDFor(t *testing.T, machineID string) string {
	t.Helper()
	m, err := h.q.GetMachine(context.Background(), machineID)
	require.NoError(t, err)
	return m.PoolID
}

// jsonField extracts a top-level string field from raw JSON.
func jsonField(t *testing.T, raw []byte, field string) string {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	s, _ := m[field].(string)
	return s
}

// Guard against accidental long-poll hangs in CI.
var _ = time.Second
