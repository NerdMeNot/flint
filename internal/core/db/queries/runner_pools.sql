-- name: ListRunnerPools :many
SELECT name, description, cpu, memory, gpu_vendor, gpu_model, gpu_count, arch, spot_preferred
FROM runner_pools WHERE ready = true ORDER BY name;

-- name: ListRunnerPoolNames :many
SELECT name FROM runner_pools;

-- name: UpsertRunnerPool :exec
INSERT INTO runner_pools (
    name, description, cpu, memory, gpu_vendor, gpu_model, gpu_count,
    arch, node_selector, tolerations, spot_preferred, spot_fallback,
    default_timeout, ready, updated_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, true, now())
ON CONFLICT (name) DO UPDATE SET
    description = EXCLUDED.description,
    cpu = EXCLUDED.cpu,
    memory = EXCLUDED.memory,
    gpu_vendor = EXCLUDED.gpu_vendor,
    gpu_model = EXCLUDED.gpu_model,
    gpu_count = EXCLUDED.gpu_count,
    arch = EXCLUDED.arch,
    node_selector = EXCLUDED.node_selector,
    tolerations = EXCLUDED.tolerations,
    spot_preferred = EXCLUDED.spot_preferred,
    spot_fallback = EXCLUDED.spot_fallback,
    default_timeout = EXCLUDED.default_timeout,
    ready = true,
    updated_at = now();

-- name: DeleteRunnerPool :exec
DELETE FROM runner_pools WHERE name = $1;

-- name: ListRunnerPoolsPaged :many
SELECT id, name, description, cpu, memory, arch, gpu_vendor, gpu_model, gpu_count, ready, created_at
FROM runner_pools ORDER BY name LIMIT $1 OFFSET $2;
