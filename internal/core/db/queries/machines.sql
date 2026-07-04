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

-- name: GetMachineByBootstrapTokenHash :one
-- Elastic registration: single-use — the caller clears the hash in the same tx.
-- 'requested' is included because provider.Create returns before the
-- requested → provisioning transition commits, and a very fast instance could
-- register in that window.
SELECT * FROM machines WHERE bootstrap_token_hash = $1
  AND status IN ('requested', 'provisioning');

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

-- name: UpdateMachineStatus :exec
-- Used only by the fleet transition chokepoint after validating the edge; callers
-- never update status directly.
UPDATE machines SET
    status = @status,
    drain_reason = COALESCE(sqlc.narg(drain_reason), drain_reason),
    provider_ref = COALESCE(sqlc.narg(provider_ref), provider_ref),
    boot_deadline_at = COALESCE(sqlc.narg(boot_deadline_at), boot_deadline_at),
    provisioned_at = CASE WHEN @status = 'provisioning' THEN now() ELSE provisioned_at END,
    idle_since     = CASE WHEN @status = 'idle' THEN now() ELSE idle_since END,
    terminated_at  = CASE WHEN @status IN ('terminated', 'failed') THEN now() ELSE terminated_at END,
    updated_at = now()
WHERE id = @id;

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
-- min_warm before draining.
SELECT m.id, m.pool_id FROM machines m
JOIN machine_pools p ON p.id = m.pool_id
WHERE m.status = 'idle'
  AND m.idle_since < now() - make_interval(secs := p.idle_ttl_seconds)
FOR UPDATE OF m SKIP LOCKED;

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
