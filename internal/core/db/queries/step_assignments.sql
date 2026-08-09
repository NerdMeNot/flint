-- name: InsertStepAssignment :one
-- Machine executor dispatch: a 'pending' assignment IS the pending-capacity queue —
-- dispatch never blocks on capacity; the fleet loop owns boot-new decisions.
INSERT INTO step_assignments (
    step_id, workflow_id, run_id, step_name, attempt, pool_id,
    cpu_millis, memory_mb, disk_gb, payload
) VALUES (
    @step_id, @workflow_id, @run_id, @step_name, @attempt, @pool_id,
    @cpu_millis, @memory_mb, @disk_gb, @payload
) RETURNING id;

-- name: ClaimPendingAssignments :many
-- Fleet scheduler input: oldest-first pending work for one pool. created_at is
-- returned so binding can report how long the work actually queued — the
-- user-visible half of "why is my build waiting".
SELECT id, step_id, workflow_id, run_id, step_name, attempt,
       cpu_millis, memory_mb, disk_gb, created_at
FROM step_assignments
WHERE pool_id = $1 AND status = 'pending'
ORDER BY created_at
LIMIT $2
FOR UPDATE SKIP LOCKED;

-- name: BindAssignment :exec
-- Scheduler → machine binding. claim_deadline_at bounds how long the agent has to
-- pick it up before the sweep unbinds it.
UPDATE step_assignments SET
    status = 'assigned', machine_id = @machine_id,
    assigned_at = now(), claim_deadline_at = @claim_deadline_at
WHERE id = @id AND status = 'pending';

-- name: ClaimAssignmentForAgent :one
-- Agent ClaimStep: atomically take the oldest assigned work for this machine.
UPDATE step_assignments SET status = 'running', started_at = now()
WHERE id = (
    SELECT sa.id FROM step_assignments sa
    WHERE sa.machine_id = @machine_id AND sa.status = 'assigned'
    ORDER BY sa.assigned_at
    LIMIT 1
    FOR UPDATE SKIP LOCKED
)
RETURNING id, run_id, step_name, attempt, payload;

-- name: FinishAssignment :execrows
-- :execrows so CompleteAssignment can detect a duplicate completion (0 rows =
-- the assignment was already finalized by an earlier delivery) and skip the
-- steps_completed bump + busy→idle transition, which would otherwise drift the
-- machine's stats and accounting on a redelivered result.
UPDATE step_assignments SET
    status = @status, error = sqlc.narg(error), finished_at = now()
WHERE id = @id AND status IN ('pending', 'assigned', 'running');

-- name: ReleaseUnclaimedAssignments :many
-- Sweep: assigned but never claimed before the deadline → back to pending. The
-- machine gets a strike (tracked by the fleet loop, not here).
UPDATE step_assignments SET
    status = 'pending', machine_id = NULL, assigned_at = NULL, claim_deadline_at = NULL
WHERE id IN (
    SELECT id FROM step_assignments
    WHERE status = 'assigned' AND claim_deadline_at < now()
    FOR UPDATE SKIP LOCKED
)
RETURNING id, machine_id;

-- name: FailMachineAssignments :many
-- Machine lost: every live assignment on it fails; the caller emits a step-result
-- signal per row so the engine's existing retry path takes over.
UPDATE step_assignments SET status = 'lost', error = @error, finished_at = now()
WHERE machine_id = @machine_id AND status IN ('assigned', 'running')
RETURNING id, step_id, workflow_id, run_id, step_name, attempt;

-- name: CancelRunAssignments :execrows
-- CleanupRun / engine cancellation: kill work that has not reached an agent yet.
UPDATE step_assignments SET status = 'cancelled', finished_at = now()
WHERE run_id = $1 AND status IN ('pending', 'assigned');

-- name: RequestAssignmentCancel :execrows
-- Running work: flag for delivery via the ExecuteStep stream or next heartbeat.
UPDATE step_assignments SET cancel_requested = true
WHERE run_id = $1 AND status = 'running';

-- name: ListCancelRequestedForMachine :many
SELECT id, run_id, step_name FROM step_assignments
WHERE machine_id = $1 AND status = 'running' AND cancel_requested = true;

-- name: ListActiveAssignmentsForMachine :many
SELECT id, step_id, run_id, step_name, attempt, status, cpu_millis, memory_mb
FROM step_assignments
WHERE machine_id = $1 AND status IN ('assigned', 'running');

-- name: MachineFreeCapacity :many
-- Scheduler input: candidate machines for a pool with their committed capacity.
-- Free = machines.cpu_millis − committed (computed by the caller); no reserved
-- counters to drift.
--
-- Run affinity used to be answered here too, by array_agg-ing every run the
-- machine had ever touched. That made the scheduler's hot path — once per pool
-- per tick — proportional to the machine's whole 90-day retention window: on a
-- 50-machine pool with 100k historical assignments it read every one of them,
-- spilled the sort to a temp file, and took ~78ms. Affinity is now answered
-- from the run side by RunAffinityHolders, which is proportional to the
-- scheduling BATCH instead. What is left here touches only live work, so
-- idx_assignments_machine_active applies and the same query costs ~0.1ms.
--
-- The status predicate must stay in the JOIN rather than a FILTER clause: in
-- the FILTER position Postgres cannot match the partial index and goes back to
-- reading everything.
SELECT m.id, m.status, m.cpu_millis, m.memory_mb, m.last_heartbeat_at, m.capacity_type,
       COALESCE(SUM(a.cpu_millis), 0)::bigint AS committed_cpu_millis,
       COALESCE(SUM(a.memory_mb), 0)::bigint AS committed_memory_mb
FROM machines m
LEFT JOIN step_assignments a
       ON a.machine_id = m.id
      AND a.status IN ('assigned', 'running')
WHERE m.pool_id = $1 AND m.status IN ('idle', 'busy')
GROUP BY m.id;

-- name: RunAffinityHolders :many
-- Scheduler input: for the runs in THIS batch, which machine already holds each
-- run's workspace. Run affinity is a hard guarantee — a run's workspace is a
-- local directory on the machine that started it — so every later step of a run
-- must land on its holder.
--
-- Asking it per-run rather than per-machine is what keeps it cheap: a batch is
-- at most schedulerBatch runs, and (run_id, status) is already indexed, so this
-- is ~2.7ms on a 100k-assignment table regardless of how much history the
-- machines have accumulated.
--
-- 'succeeded'/'failed' assignments still pin a run (the workspace directory
-- outlives the assignment that created it); 'lost' (machine death — retries
-- start fresh) and 'cancelled' do not. DISTINCT ON + ORDER BY created_at picks
-- the EARLIEST holder, which is the machine the workspace was established on.
-- Only idle/busy machines in this pool can hold work; a dead machine's runs are
-- deliberately unpinned so their retries can start somewhere new.
SELECT DISTINCT ON (a.run_id) a.run_id, a.machine_id
FROM step_assignments a
JOIN machines m ON m.id = a.machine_id
WHERE a.run_id = ANY(@run_ids::uuid[])
  AND a.status IN ('assigned', 'running', 'succeeded', 'failed')
  AND m.pool_id = @pool_id
  AND m.status IN ('idle', 'busy')
ORDER BY a.run_id, a.created_at;

-- name: GetAssignment :one
SELECT * FROM step_assignments WHERE id = $1;

-- name: ListRunAssignments :many
-- Run placement panel: which machine executed each step, and when.
SELECT a.*, m.instance_type, m.capacity_type, m.price_per_hour_usd
FROM step_assignments a
LEFT JOIN machines m ON m.id = a.machine_id
WHERE a.run_id = $1
ORDER BY a.created_at;

-- name: PendingAssignmentDemand :many
-- Fleet scheduler input: unbound demand per pool (ALL pending, incl. work pinned
-- to an existing holder — the scheduler still has to place those on their holder).
SELECT pool_id, count(*) AS n,
       COALESCE(SUM(cpu_millis), 0)::bigint AS cpu_millis,
       COALESCE(SUM(memory_mb), 0)::bigint AS memory_mb
FROM step_assignments WHERE status = 'pending'
GROUP BY pool_id;

-- name: PendingProvisioningDemand :many
-- Fleet provisioner input: pending demand a NEW machine can actually serve.
-- Excludes pending assignments whose run is already pinned by HARD affinity to a
-- live (idle/busy) holder machine in the same pool — booting can't take that work
-- (it can only run on the holder), so it must not inflate the provisioning
-- deficit. The affinity rule mirrors MachineFreeCapacity: assigned/running/
-- succeeded/failed pin a run to its workspace machine; lost/cancelled don't.
SELECT sa.pool_id, count(*) AS n
FROM step_assignments sa
WHERE sa.status = 'pending'
  AND NOT EXISTS (
    SELECT 1
    FROM step_assignments held
    JOIN machines m ON m.id = held.machine_id
    WHERE held.run_id = sa.run_id
      AND m.pool_id = sa.pool_id
      AND m.status IN ('idle', 'busy')
      AND held.status IN ('assigned', 'running', 'succeeded', 'failed')
  )
GROUP BY sa.pool_id;

-- name: PendingProvisioningDemandByGroup :many
-- Same provisionable demand as PendingProvisioningDemand, but split by the run's
-- (branch, event) so the fleet can resolve per-branch/event economics policy
-- (main → stable, PRs → interruptible) before quoting. Joins pipeline_runs for
-- the branch/trigger_type; a run with a null branch/type collapses to '' (the
-- wildcard context, which only matches an override whose field is also empty).
-- LEFT JOIN so demand never vanishes when a run row is missing (it collapses to
-- the '' wildcard context instead) — an INNER JOIN would silently drop such
-- assignments from the provisioning deficit.
SELECT sa.pool_id,
       COALESCE(pr.branch, '') AS branch,
       COALESCE(pr.trigger_type, '') AS event,
       count(*) AS n
FROM step_assignments sa
LEFT JOIN pipeline_runs pr ON pr.id = sa.run_id
WHERE sa.status = 'pending'
  AND NOT EXISTS (
    SELECT 1
    FROM step_assignments held
    JOIN machines m ON m.id = held.machine_id
    WHERE held.run_id = sa.run_id
      AND m.pool_id = sa.pool_id
      AND m.status IN ('idle', 'busy')
      AND held.status IN ('assigned', 'running', 'succeeded', 'failed')
  )
GROUP BY sa.pool_id, pr.branch, pr.trigger_type;

-- name: CleanupOldStepAssignments :exec
-- Retention: terminal assignments past the window. They do NOT cascade from
-- DeleteOldRuns (step_assignments has no FK to runs — deliberate, to keep the
-- hot dispatch path off the runs lock), and each carries the fattest JSONB
-- payload in the operational path, so this is the top bloat vector. Batched, on
-- the same window as runs retention.
DELETE FROM step_assignments WHERE id IN (
    SELECT id FROM step_assignments
    WHERE status IN ('succeeded', 'failed', 'cancelled', 'lost')
      AND finished_at < now() - make_interval(days => sqlc.arg(retention_days)::int)
    LIMIT 2000
);

-- name: TerminalRunIDs :many
-- Heartbeat GC input: of the run ids resident on a machine's disk, which have
-- reached a terminal state (their workspace dirs are safe to delete).
SELECT id FROM pipeline_runs
WHERE id = ANY(sqlc.arg(run_ids)::uuid[])
  AND status IN ('succeeded', 'failed', 'cancelled');

-- name: StaleRunningAssignmentsForMachine :many
-- Agent-restart recovery: 'running' assignments the machine's heartbeat no
-- longer claims, past a grace period since they started.
SELECT id, step_id, workflow_id, run_id, step_name, attempt
FROM step_assignments
WHERE machine_id = sqlc.arg(machine_id)
  AND status = 'running'
  AND started_at < now() - make_interval(secs := sqlc.arg(grace_secs)::double precision)
  AND NOT (id = ANY(sqlc.arg(active_ids)::uuid[]));

-- name: FailAssignment :one
-- Fails one assignment (agent-reported death or restart reconcile), returning
-- the identifiers the caller needs to emit the step-result signal.
UPDATE step_assignments SET status = 'failed', error = sqlc.arg(error), finished_at = now()
WHERE id = sqlc.arg(id) AND status IN ('assigned', 'running')
RETURNING id, step_id, workflow_id, run_id, step_name, attempt;
