-- name: InsertMachine :one
-- Elastic path: the fleet provisioner pre-allocates the row in 'requested' with a
-- minted bootstrap token before calling provider.Create (Create is idempotent per
-- machine id, so a crash between insert and Create is safe to retry).
INSERT INTO machines (
    pool_id, status, provider, instance_type, region, zone, capacity_type,
    price_per_hour_usd, accepted_offer, cpu_millis, memory_mb, disk_gb, arch,
    bootstrap_token_hash
) VALUES (
    @pool_id, 'requested', @provider, @instance_type, @region, @zone, @capacity_type,
    @price_per_hour_usd, @accepted_offer, @cpu_millis, @memory_mb, @disk_gb, @arch,
    @bootstrap_token_hash
) RETURNING id;

-- name: InsertStaticMachine :one
-- Static path: RegisterMachine with a pool join token creates the machine directly
-- in 'idle' with the agent's self-reported capacity.
INSERT INTO machines (
    pool_id, status, provider, cpu_millis, memory_mb, disk_gb, arch, os,
    labels, hostname, agent_version, agent_token_hash,
    heartbeat_interval_seconds, last_heartbeat_at, heartbeat_expires_at,
    registered_at, idle_since
) VALUES (
    @pool_id, 'idle', 'static', @cpu_millis, @memory_mb, @disk_gb, @arch, @os,
    @labels, @hostname, @agent_version, @agent_token_hash,
    @heartbeat_interval_seconds, now(), @heartbeat_expires_at,
    now(), now()
) RETURNING id;

-- name: GetMachine :one
SELECT * FROM machines WHERE id = $1;

-- name: LockMachine :one
-- The machine row, locked for the caller's transaction. Used where a transition
-- must be validated against the CURRENT status rather than one read earlier: an
-- agent RPC carries the row captured when it authenticated, and the fleet may
-- have moved the machine (idle → busy) in the meantime. Validating against that
-- stale snapshot silently dropped the transition.
SELECT * FROM machines WHERE id = $1 FOR UPDATE;

-- name: ClaimDrainedMachines :many
-- Drained machines with no work left. Draining means "finish what you have and
-- stop"; once nothing is left, the machine has done what was asked and should be
-- terminated. Without this nothing completed a drain: the machine sat in
-- 'draining' billing until its agent stopped heartbeating, was then marked
-- 'lost' — an error state, for a graceful operation — and reaped by reconcile.
--
-- Static machines are excluded for the same reason scale-down excludes them:
-- Flint doesn't own their power button, so 'draining' is their resting state
-- until an operator acts.
SELECT m.id, m.pool_id FROM machines m
WHERE m.status = 'draining'
  AND m.provider <> 'static'
  AND NOT EXISTS (
    SELECT 1 FROM step_assignments a
    WHERE a.machine_id = m.id AND a.status IN ('assigned', 'running')
  )
FOR UPDATE OF m SKIP LOCKED;

-- name: GetMachineByBootstrapTokenHash :one
-- Elastic registration: single-use — the caller clears the hash in the same tx.
-- 'requested' is included because provider.Create returns before the
-- requested → provisioning transition commits, and a very fast instance could
-- register in that window.
--
-- FOR UPDATE because the caller transitions the machine FROM the status this
-- returns, and that transition is guarded by `WHERE status = @from_status`.
-- Read without the lock, the status could change between this select and the
-- update — exactly what happens when a fast instance registers while the
-- provisioner is still committing requested → provisioning — and registration
-- failed with a lost-race error instead of succeeding. Locking the row makes
-- the two orderings deterministic: whoever gets the lock first wins, and the
-- loser observes the committed status rather than a stale one.
SELECT * FROM machines WHERE bootstrap_token_hash = $1
  AND status IN ('requested', 'provisioning')
FOR UPDATE;

-- name: GetMachineByAgentTokenHash :one
-- Auth interceptor lookup for all post-registration agent calls.
SELECT * FROM machines
WHERE agent_token_hash = $1
  AND status NOT IN ('terminated', 'failed');

-- name: CompleteMachineRegistration :exec
-- provisioning → idle: consume the bootstrap token, mint the agent token, record
-- the agent's self-reported identity/capacity. The status transition itself goes
-- through the fleet transition chokepoint in the same tx.
UPDATE machines SET
    bootstrap_token_hash = NULL,
    agent_token_hash = @agent_token_hash,
    cpu_millis = @cpu_millis, memory_mb = @memory_mb, disk_gb = @disk_gb,
    arch = @arch, os = @os, labels = @labels, hostname = @hostname,
    agent_version = @agent_version,
    last_heartbeat_at = now(), heartbeat_expires_at = @heartbeat_expires_at,
    registered_at = now(), boot_deadline_at = NULL,
    updated_at = now()
WHERE id = @id;

-- name: TouchMachineHeartbeat :exec
UPDATE machines SET
    last_heartbeat_at = now(),
    heartbeat_expires_at = @heartbeat_expires_at,
    agent_version = @agent_version,
    updated_at = now()
WHERE id = @id;

-- name: UpdateMachineStatus :execrows
-- Used only by the fleet transition chokepoint after validating the edge; callers
-- never update status directly. The `from` guard makes the transition optimistic:
-- the row moves only if it's still in the expected state, so 0 rows affected means
-- another actor already moved it (a lost race). The chokepoint surfaces that so the
-- caller can skip or roll back, rather than clobbering a concurrent transition
-- (last-writer-wins would corrupt busy/idle accounting under multi-replica load).
UPDATE machines SET
    status = @status,
    drain_reason = COALESCE(sqlc.narg(drain_reason), drain_reason),
    provider_ref = COALESCE(sqlc.narg(provider_ref), provider_ref),
    boot_deadline_at = COALESCE(sqlc.narg(boot_deadline_at), boot_deadline_at),
    provisioned_at = CASE WHEN @status = 'provisioning' THEN now() ELSE provisioned_at END,
    idle_since     = CASE WHEN @status = 'idle' THEN now() ELSE idle_since END,
    terminated_at  = CASE WHEN @status IN ('terminated', 'failed') THEN now() ELSE terminated_at END,
    updated_at = now()
WHERE id = @id AND status = @from_status;

-- name: ClaimExpiredBootDeadlines :many
-- Sweep: machines that never registered before their boot deadline.
SELECT id, pool_id, provider, provider_ref, status FROM machines
WHERE boot_deadline_at < now() AND status IN ('requested', 'provisioning')
FOR UPDATE SKIP LOCKED;

-- name: ClaimExpiredHeartbeats :many
-- Sweep: machines whose heartbeat lease lapsed → lost.
SELECT id, pool_id, provider, provider_ref, status FROM machines
WHERE heartbeat_expires_at < now() AND status IN ('idle', 'busy', 'draining')
FOR UPDATE SKIP LOCKED;

-- name: ListPoolMachines :many
SELECT * FROM machines WHERE pool_id = $1
  AND status NOT IN ('terminated', 'failed')
ORDER BY requested_at;

-- name: ListMachinesPaged :many
SELECT * FROM machines
WHERE (sqlc.narg(pool_id)::uuid IS NULL OR pool_id = sqlc.narg(pool_id))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
ORDER BY requested_at DESC LIMIT $1 OFFSET $2;

-- name: CountPoolMachinesByStatus :many
-- Fleet provisioner input: live machine counts per status for one pool.
SELECT status, count(*) AS n FROM machines
WHERE pool_id = $1 AND status IN ('requested', 'provisioning', 'idle', 'busy', 'draining')
GROUP BY status;

-- name: ClaimIdleMachinesPastTTL :many
-- Scale-down candidates: idle beyond the pool TTL. The provisioner re-checks
-- min_warm before draining. The NOT EXISTS guard is essential: a machine can be
-- status='idle' yet still hold a live assignment (busy/idle accounting can drift,
-- or work was bound just after it went idle) — terminating it would kill running
-- work, so those are never candidates.
SELECT m.id, m.pool_id FROM machines m
JOIN machine_pools p ON p.id = m.pool_id
WHERE m.status = 'idle'
  AND m.idle_since < now() - make_interval(secs := p.idle_ttl_seconds)
  AND NOT EXISTS (
    SELECT 1 FROM step_assignments a
    WHERE a.machine_id = m.id AND a.status IN ('assigned', 'running')
  )
FOR UPDATE OF m SKIP LOCKED;

-- name: FleetInventory :many
-- Metrics input: the live fleet's shape in one query — machine counts and the
-- committed $/hour they represent, grouped so a dashboard can slice by pool,
-- state, and reliability class. This is the number the product promises to make
-- legible, so it is read from the machines table (authoritative) rather than
-- accumulated in-process, where it would drift across restarts and replicas.
-- Terminal machines are excluded: they cost nothing and would grow forever.
SELECT p.name AS pool_name,
       m.status,
       COALESCE(m.capacity_type, 'unknown')::text AS capacity_type,
       count(*) AS n,
       COALESCE(SUM(m.price_per_hour_usd), 0)::float8 AS hourly_cost_usd
FROM machines m
JOIN machine_pools p ON p.id = m.pool_id
WHERE m.status NOT IN ('terminated', 'failed')
GROUP BY p.name, m.status, COALESCE(m.capacity_type, 'unknown');

-- name: IncrementMachineStepsCompleted :exec
UPDATE machines SET steps_completed = steps_completed + 1, updated_at = now() WHERE id = $1;

-- name: InsertMachineEvent :exec
INSERT INTO machine_events (machine_id, event_type, from_status, to_status, actor, reason, metadata)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListMachineEvents :many
SELECT * FROM machine_events WHERE machine_id = $1 ORDER BY created_at DESC LIMIT $2;

-- name: ListProviderMachines :many
-- Reconciliation input: this provider's machines in every non-final state.
SELECT id, pool_id, status, provider_ref FROM machines
WHERE provider = $1 AND status NOT IN ('terminated', 'failed');

-- name: SetMachineProviderRef :exec
-- Records the provider instance id without touching status — used when the
-- agent registered before the provisioning transition could commit.
UPDATE machines SET provider_ref = $2, provisioned_at = COALESCE(provisioned_at, now()), updated_at = now()
WHERE id = $1;

-- name: CleanupOldMachineEvents :exec
-- Prune machine transition history older than 30 days (mirrors engine_events).
-- Heartbeat-driven busy⇄idle churn makes this high-volume on a live fleet.
-- Batched so a backlog can't stall a sweep with one giant DELETE.
DELETE FROM machine_events WHERE id IN (
    SELECT id FROM machine_events
    WHERE created_at < now() - interval '30 days'
    LIMIT 5000
);
