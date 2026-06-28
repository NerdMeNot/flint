-- name: ListRunnerPools :many
-- Full rows — used by the worker registry loader, compile-time validation, and the
-- pool catalog. Returns everything needed to reconstruct a PoolSpec.
SELECT name, description, cpu, memory, gpu_vendor, gpu_model, gpu_count, arch,
       node_selector, tolerations, default_timeout,
       service_account_name, workspace_mode, workspace_storage_class, workspace_size,
       run_as_non_root, mode, managed_spec, is_default
FROM runner_pools WHERE ready = true ORDER BY name;

-- name: GetRunnerPool :one
SELECT name, description, cpu, memory, gpu_vendor, gpu_model, gpu_count, arch,
       node_selector, tolerations, default_timeout,
       service_account_name, workspace_mode, workspace_storage_class, workspace_size,
       run_as_non_root, mode, managed_spec, is_default
FROM runner_pools WHERE name = $1;

-- name: ListRunnerPoolNames :many
SELECT name FROM runner_pools;

-- name: UpsertRunnerPool :exec
INSERT INTO runner_pools (
    name, description, cpu, memory, gpu_vendor, gpu_model, gpu_count,
    arch, node_selector, tolerations,
    default_timeout, service_account_name, workspace_mode, workspace_storage_class,
    workspace_size, run_as_non_root, mode, managed_spec, ready, updated_at
) VALUES (
    @name, @description, @cpu, @memory, @gpu_vendor, @gpu_model, @gpu_count,
    @arch, @node_selector, @tolerations,
    @default_timeout, @service_account_name, @workspace_mode, @workspace_storage_class,
    @workspace_size, @run_as_non_root, @mode, @managed_spec, true, now()
)
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
    default_timeout = EXCLUDED.default_timeout,
    service_account_name = EXCLUDED.service_account_name,
    workspace_mode = EXCLUDED.workspace_mode,
    workspace_storage_class = EXCLUDED.workspace_storage_class,
    workspace_size = EXCLUDED.workspace_size,
    run_as_non_root = EXCLUDED.run_as_non_root,
    mode = EXCLUDED.mode,
    managed_spec = EXCLUDED.managed_spec,
    ready = true,
    updated_at = now();

-- name: DeleteRunnerPool :exec
DELETE FROM runner_pools WHERE name = $1;

-- name: ListRunnerPoolsPaged :many
SELECT id, name, description, cpu, memory, arch, gpu_vendor, gpu_model, gpu_count,
       mode, managed_spec, node_selector, tolerations, is_default, ready, created_at
FROM runner_pools ORDER BY name LIMIT $1 OFFSET $2;

-- name: SetDefaultRunnerPool :exec
-- Promotes one pool to default and demotes all others atomically.
UPDATE runner_pools SET is_default = (name = @name), updated_at = now();

-- name: CountDefaultRunnerPools :one
SELECT count(*) FROM runner_pools WHERE is_default = true;
