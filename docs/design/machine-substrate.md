# Machine Substrate — Flint off Kubernetes

**Status:** Landed (July 2026). Supersedes the Kubernetes execution sections of
[flint-family-architecture.md](flint-family-architecture.md).

## Why

Kubernetes was Flint's original execution substrate: steps ran as k8s Jobs,
elasticity came from Karpenter, workspaces from PVCs and per-run pods. That
bought scheduling for the price of setup complexity (a cluster as an install
prerequisite), latency (pod start, image pull, PVC attach on every step), and a
constrained data plane (everything round-trips object storage because pod disks
vanish). Every fast CI product — Depot, Blacksmith, Buildkite — runs raw
VMs/metal for exactly these reasons.

Flint now runs steps on **machines**: VMs or bare metal operated by a
persistent `flint-agent` daemon. Clouds integrate through a deliberately small
provider interface, and Flint's economics decisions (warm vs boot, spot vs
on-demand, which instance) are recorded in a ledger the user can audit. K8s may
return someday — as just another compute provider, never as the substrate.

## Architecture

```
flint server (one binary: flint server --mode all)
├── Hertz HTTP API (existing)  +  gRPC AgentService (:9443)
├── engine.Loop     — unchanged workflow engine; container steps dispatch via
│                     the machine executor → step_assignments rows
├── fleet.Loop      — schedule (bind pending → machines, warmth-affine),
│                     provision (Quote → rank by pool objective → Create),
│                     scale down (idle TTL above minWarm), sweeps (boot
│                     deadline, heartbeat lease, unclaimed release),
│                     reconcile (provider truth: zombies, spot kills)
└── Postgres        — machines, machine_events (no-FK event log),
                      step_assignments, machine_pools, compute_providers,
                      fleet_decisions

flint-agent (per machine, linux, static binary)
├── register (pool join token / cloud-init bootstrap token → machine token)
├── heartbeat every 10s (lease ×3; drain/shutdown/cancel/GC piggyback)
├── ClaimStep long-poll → execute → ReportStepComplete (task-token verified,
│   through the engine's idempotent CompleteStep chokepoint)
├── runtimes: containerd (bundled or system socket; container-per-step,
│   image layer cache, cgroup limits) | hostshell (dev only, no isolation)
└── data locality: git bare mirrors (fetch-delta checkouts), layered
    local→S3 cache (write-through), per-run workspace dirs, GC via heartbeat
```

### Machine lifecycle

`requested → provisioning → idle ⇄ busy → draining → terminating → terminated`
plus `failed` (boot never completed) and `lost` (lease expired / provider says
gone). Every edge goes through a transition chokepoint that validates against
the allowed-edge table and appends `machine_events` in the same transaction —
the same pattern as the engine's step state machine. A lost machine's live
assignments fail through the engine's existing `step-result` signal seam, so
retry policy applies to a spot reclaim exactly as it did to a crashed pod.

### Provider contract (`pkg/compute`)

```go
Quote(ctx, Requirements) ([]Offer, error) // priced ways to satisfy a shape
Create(ctx, Offer, Bootstrap) (MachineRef, error) // idempotent per MachineID
Destroy(ctx, MachineRef) error            // idempotent
List(ctx) ([]MachineRef, error)           // reconciliation source of truth
```

A provider doesn't have to be a cloud — anything that can make an agent
register within the boot deadline qualifies. `computetest.RunConformance` is
the certification kit; `computetest.Fake` drives the fleet tests. Built-ins:
`static` (bring-your-own machines join via pool tokens) and `aws` (EC2:
embedded on-demand catalog + live spot quotes, RunInstances with cloud-init
bootstrap, tag-scoped reconciliation, IMDS spot-drain from the agent side).

### Economics: aids, not automation

Users state policy on the pool: `objective` (cost | latency | balanced),
`minWarm`, `maxMachines`, `idleTtl`, `capacityType`, per-branch overrides.
Zero standing infra is simply `minWarm: 0` — the speed compromise belongs to
the user. Flint optimizes within that policy and writes every provision/
terminate/drain/no-capacity decision to `fleet_decisions` with its inputs, the
chosen offer, the ranked alternatives, and the backfilled outcome (boot
seconds, timeouts, termination). Surfaces: `GET /machines` (+cost to date),
`GET /decisions`, `GET /runs/:id/placement` (queue wait, instance, $/hr per
step).

### Security model (v1, stated honestly)

Container-per-step on a shared kernel isolates workloads, not hostile
tenants. Per-run 0700 workspace dirs are wiped after the run; secrets are
pulled per step over gRPC (assignment-bound authorization), written to a
0600 env file sourced by the step shell, shredded after — never the agent's
process env, never the container spec. Hard isolation option: a pool with
`ephemeral: true` semantics (one run → destroy machine). MicroVMs are a
future runtime behind the same `Runtime` interface.

## Deliberate v1 boundaries

- **Networking:** containerd steps use host networking; per-step CNI netns +
  service containers are the next runtime increment. `services:` blocks are
  not yet honored by the machine path.
- **Workspace across machines:** run affinity is a scheduler GUARANTEE, not a
  preference — a run's workspace is a local directory on the machine that
  started it, so every later step of that run lands there (or waits pending
  until it frees up). If the machine dies, its assignments fail through the
  machine-lost path and retries start fresh. There is no cross-machine
  workspace sync; the legacy per-run workspace pod, sidecar agent, and HTTP
  `/internal` endpoints have been removed.
- **Agent download:** cloud-init pulls release binaries from GitHub; a
  self-hosted mirror override belongs in provider config.
- **dind/buildkit:** `privileged: true` per step exists; a first-class
  `docker: true` with rootless buildkit is roadmap.

## Verification

- `internal/core/fleet` integration suite (throwaway Postgres via
  `testutil/pgtest`): registration, lease expiry/reappearance, claim
  long-poll, self-drain, elastic provision→register→schedule→scale-down,
  minWarm floor, spot-kill reconcile, boot timeout with ledger backfill.
- `TestEndToEnd_PipelineOnRealAgent`: a multi-step pipeline (including a
  group-steps job) runs engine → executor → scheduler → a real `agentd`
  daemon → completion, asserting outputs, warmth affinity, logs, and the
  machine returning to idle.
- `pkg/compute/awsec2` passes the conformance suite over a mocked EC2 API.
