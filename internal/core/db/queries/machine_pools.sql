-- name: ListMachinePools :many
-- Full rows — used by the fleet pool registry loader, compile-time validation, and
-- the pool catalog. Returns everything needed to reconstruct a PoolSpec.
SELECT id, name, description, provider, arch, cpu, memory, disk,
       gpu_vendor, gpu_model, gpu_count, instance_types, regions,
       capacity_type, objective, min_warm, max_machines, idle_ttl_seconds,
       overrides, hourly_cost, default_timeout, is_default
FROM machine_pools WHERE ready = true ORDER BY name;

-- name: GetMachinePool :one
SELECT id, name, description, provider, arch, cpu, memory, disk,
       gpu_vendor, gpu_model, gpu_count, instance_types, regions,
       capacity_type, objective, min_warm, max_machines, idle_ttl_seconds,
       overrides, hourly_cost, default_timeout, is_default
FROM machine_pools WHERE name = $1;

-- name: ListMachinePoolNames :many
SELECT name FROM machine_pools;

-- name: UpsertMachinePool :exec
INSERT INTO machine_pools (
    name, description, provider, arch, cpu, memory, disk,
    gpu_vendor, gpu_model, gpu_count, instance_types, regions,
    capacity_type, objective, min_warm, max_machines, idle_ttl_seconds,
    overrides, hourly_cost, default_timeout, ready, updated_at
) VALUES (
    @name, @description, @provider, @arch, @cpu, @memory, @disk,
    @gpu_vendor, @gpu_model, @gpu_count, @instance_types, @regions,
    @capacity_type, @objective, @min_warm, @max_machines, @idle_ttl_seconds,
    @overrides, @hourly_cost, @default_timeout, true, now()
)
ON CONFLICT (name) DO UPDATE SET
    description = EXCLUDED.description,
    provider = EXCLUDED.provider,
    arch = EXCLUDED.arch,
    cpu = EXCLUDED.cpu,
    memory = EXCLUDED.memory,
    disk = EXCLUDED.disk,
    gpu_vendor = EXCLUDED.gpu_vendor,
    gpu_model = EXCLUDED.gpu_model,
    gpu_count = EXCLUDED.gpu_count,
    instance_types = EXCLUDED.instance_types,
    regions = EXCLUDED.regions,
    capacity_type = EXCLUDED.capacity_type,
    objective = EXCLUDED.objective,
    min_warm = EXCLUDED.min_warm,
    max_machines = EXCLUDED.max_machines,
    idle_ttl_seconds = EXCLUDED.idle_ttl_seconds,
    overrides = EXCLUDED.overrides,
    hourly_cost = EXCLUDED.hourly_cost,
    default_timeout = EXCLUDED.default_timeout,
    ready = true,
    updated_at = now();

-- name: DeleteMachinePool :exec
DELETE FROM machine_pools WHERE name = $1;

-- name: ListMachinePoolsPaged :many
SELECT id, name, description, provider, arch, cpu, memory, disk,
       gpu_vendor, gpu_model, gpu_count, instance_types, regions,
       capacity_type, objective, min_warm, max_machines, idle_ttl_seconds,
       overrides, hourly_cost, is_default, ready, created_at
FROM machine_pools ORDER BY name LIMIT $1 OFFSET $2;

-- name: SetDefaultMachinePool :exec
-- Promotes one pool to default and demotes all others atomically.
UPDATE machine_pools SET is_default = (name = @name), updated_at = now();

-- name: CountDefaultMachinePools :one
SELECT count(*) FROM machine_pools WHERE is_default = true;

-- name: SetPoolJoinTokenHash :exec
-- Static pools: stores the sha256 of a newly minted (or rotated) agent join token.
UPDATE machine_pools SET join_token_hash = $2, updated_at = now() WHERE name = $1;

-- name: GetPoolByJoinTokenHash :one
-- Registration path for static-pool agents presenting a join token.
SELECT id, name, provider, arch FROM machine_pools
WHERE join_token_hash = $1 AND ready = true;

-- name: GetMachinePoolByID :one
SELECT id, name, provider, arch, idle_ttl_seconds, min_warm, max_machines
FROM machine_pools WHERE id = $1;

-- name: PoolInsightMachines :one
-- Pool insights (7d): machine-hours, spend, boots, boot p50, price p50, and
-- spot interruptions — arithmetic over machine lifecycles clipped to the
-- window, no simulation. Spend uses each machine's real price when priced.
SELECT
  COALESCE(SUM(EXTRACT(EPOCH FROM (
    LEAST(COALESCE(m.terminated_at, now()), now())
    - GREATEST(m.requested_at, now() - interval '7 days')
  )) / 3600.0), 0)::float8 AS machine_hours,
  COALESCE(SUM(
    COALESCE(m.price_per_hour_usd, 0) * (EXTRACT(EPOCH FROM (
      LEAST(COALESCE(m.terminated_at, now()), now())
      - GREATEST(m.requested_at, now() - interval '7 days')
    )) / 3600.0)::numeric
  ), 0)::float8 AS spend_usd,
  COUNT(*) FILTER (WHERE m.registered_at >= now() - interval '7 days')::bigint AS boots,
  COALESCE(percentile_cont(0.5) WITHIN GROUP (
    ORDER BY EXTRACT(EPOCH FROM (m.registered_at - m.requested_at))
  ) FILTER (WHERE m.registered_at >= now() - interval '7 days'), 0)::float8 AS boot_p50_secs,
  COALESCE(percentile_cont(0.5) WITHIN GROUP (
    ORDER BY m.price_per_hour_usd::float8
  ) FILTER (WHERE m.price_per_hour_usd IS NOT NULL), 0)::float8 AS price_p50_usd,
  COUNT(*) FILTER (
    WHERE m.status = 'lost' AND m.capacity_type = 'spot'
      AND m.updated_at >= now() - interval '7 days'
  )::bigint AS interruptions
FROM machines m
WHERE m.pool_id = $1
  AND COALESCE(m.terminated_at, now()) >= now() - interval '7 days';

-- name: PoolInsightAssignments :one
-- Pool insights (7d): queue waits and the warm-hit rate. A warm hit is an
-- assignment whose machine was already registered when the work arrived — the
-- run never waited on a boot.
SELECT
  COUNT(*)::bigint AS total,
  COUNT(*) FILTER (WHERE m.registered_at < a.created_at)::bigint AS warm_hits,
  COALESCE(percentile_cont(0.5) WITHIN GROUP (
    ORDER BY EXTRACT(EPOCH FROM (a.started_at - a.created_at))
  ), 0)::float8 AS queue_p50_secs,
  COALESCE(percentile_cont(0.95) WITHIN GROUP (
    ORDER BY EXTRACT(EPOCH FROM (a.started_at - a.created_at))
  ), 0)::float8 AS queue_p95_secs,
  COALESCE(percentile_cont(0.5) WITHIN GROUP (
    ORDER BY EXTRACT(EPOCH FROM (a.started_at - a.created_at))
  ) FILTER (WHERE m.registered_at < a.created_at), 0)::float8 AS warm_queue_p50_secs,
  COALESCE(percentile_cont(0.5) WITHIN GROUP (
    ORDER BY EXTRACT(EPOCH FROM (a.started_at - a.created_at))
  ) FILTER (WHERE m.registered_at >= a.created_at), 0)::float8 AS cold_queue_p50_secs
FROM step_assignments a
JOIN machines m ON m.id = a.machine_id
WHERE a.pool_id = $1
  AND a.created_at >= now() - interval '7 days'
  AND a.started_at IS NOT NULL;
