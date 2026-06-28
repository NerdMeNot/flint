# Runner Pools & the Autoscaling Add-on

Status: design / proposal. Scope: how Flint selects compute for CI step pods, and an
**optional** add-on that lets Flint own ("manage") elastic CI node pools as an
abstraction over Karpenter. Honest about preconditions and scope.

## Context

A Flint job is a pod (jobs → steps; one job = one Kubernetes Job/pod). At dispatch
the k8s executor resolves the job's pool by name (`runner:`) from the in-memory
`runner.Registry`, and stamps the pool's `nodeSelector`/`tolerations`/resources/SA
onto the pod (`runner.MergeIntoJob`). Flint **provisions no nodes** — it references
selectors that target machines that already exist or that a node-autoprovisioner
(Karpenter, Cluster Autoscaler, GKE NAP) creates.

Today a RunnerPool is a *record of selectors*: the actual node pool (e.g. a Karpenter
`NodePool`) is created out-of-band, and the Flint RunnerPool restates the matching
selectors/taints to give them a Flint name. That duplication is the thing this design
removes for teams that want it — by letting Flint **generate and own** the Karpenter
`NodePool` from a single Flint pool definition — while keeping the zero-dependency
"reference an existing pool" path as the default.

## Goals

1. **Two clear modes**, one pool concept:
   - **Reference** (default, no add-on): a pool is selectors/tolerations/resources that
     target existing nodes. Works on any cluster (static nodes, Cluster Autoscaler,
     Karpenter, anything). Zero preconditions.
   - **Managed** (optional add-on): Flint reconciles a Karpenter `NodePool` +
     `EC2NodeClass` from the pool definition — an efficient, scale-to-zero, CI-tuned
     elastic pool. One source of truth; no out-of-band NodePool.
2. **Pipeline UX** that lets authors clearly pick the pool per job (with a pipeline
   default) and **optionally** set per-pod (per-job) resource requests within it.
3. **Fail fast**: validate pool/resource choices at pipeline-compile time, not as a pod
   that hangs `Pending` forever.

## Non-goals / scope discipline

- **Flint does not install Karpenter.** Managed mode *requires* a Karpenter-capable
  provisioner already present (EKS Auto Mode, AKS NAP, or a self-installed controller).
  Flint detects it and refuses managed mode with a clear error if absent. Installing /
  upgrading Karpenter is cluster bootstrap, owned by the platform team.
- **AWS + Karpenter first.** Karpenter is mature on AWS (`EC2NodeClass`) and GA on Azure
  via AKS NAP (`AKSNodeClass`) — same `NodePool` core, different NodeClass. GCP is *not*
  Karpenter (GKE NAP + ComputeClasses, a different API). So: implement behind a thin
  provisioner seam, ship AWS first, Azure is a small follow-on (swap the NodeClass
  renderer), GKE is a separate future backend — not a reshape of the abstraction.
- **No generic non-k8s compute backends.** This is a small, bounded set of k8s-native
  autoprovisioner targets, not an open-ended plugin system.
- Flint never manages another controller's NodePools — only the ones it created
  (ownership label + GC).

## Architecture

**Core Flint (no add-on):** reference-mode pools. The engine resolves `runner: name` →
selectors → pod, exactly as today. A cluster-wide **default pool** covers jobs that
don't specify one.

**Autoscaling add-on (optional component):** a controller + a cloud provisioner backend.
When a pool is `mode: managed`, the add-on reconciles a Karpenter `NodePool` +
`EC2NodeClass` to match, and mirrors the derived dev-facing profile + selectors back into
the pool registry/DB.

**Key property — the engine is mode-agnostic.** At dispatch, reference and managed pools
look identical: both resolve to `nodeSelector`/`tolerations`/resources. The only
difference is that a managed pool *also* has a controller keeping a Karpenter NodePool
alive behind that name. So pipelines, the executor, and validation never branch on mode.

```
 pool definition (name, mode, profile/intent)         ← admin (API/UI), source of truth
        │
        ├── reference mode → just selectors/tolerations ──┐
        │                                                  ├─► runner.Registry / DB
        └── managed mode → add-on reconciles ──────────────┘        │
              Karpenter NodePool + EC2NodeClass                      │
              (Karpenter provisions nodes JIT)                       ▼
                                                    engine: runner:name → stamp pod spec
```

## Preconditions for the add-on (documented, detected, enforced)

Managed mode is gated; on enable Flint checks and errors clearly if unmet:
1. **A Karpenter provisioner is running** — the `NodePool`/`EC2NodeClass` CRDs exist and a
   Karpenter (or EKS Auto Mode / AKS NAP) controller is reconciling them.
2. **A cluster provisioning profile** is configured *once* by the platform team — the
   account-level inputs a NodeClass needs that Flint can't invent: subnet selectors,
   security-group selectors, the node instance IAM role / instance profile, AMI family,
   and any discovery tags. This is add-on config (Helm values or a singleton record), not
   per-pool.
3. **RBAC** for the add-on to create/update/delete `NodePool`/`EC2NodeClass` (scoped to
   resources it owns).

Without these, reference mode still works fully — the add-on simply isn't enabled.

## Pool model & source of truth

One `RunnerPool` concept with a `mode` discriminator. **Canonical management is the Flint
API/UI** (admin → Runners), consistent with how projects/workspaces/etc. are now managed
— not a CRD. The add-on controller reconciles managed pools from the DB via the existing
LISTEN/NOTIFY + worker loop. (A CRD/GitOps adapter for infra teams who prefer pools in
git is a possible follow-up, deliberately deferred to keep one source of truth.)

A pool definition has:
- **Identity**: `name`, `description`, `mode: reference | managed`.
- **Dev-facing profile** (what pipelines see / validate against): `arch`, `cpu`/`memory`
  envelope, `gpu` (vendor + model(s)), a **default request** for jobs that don't specify.
- **Reference mode**: `nodeSelector`, `tolerations` (targets existing nodes).
- **Managed mode (intent → Karpenter)**:
  - capacity envelope: instance families *or* cpu/memory size ranges, arch, GPU type
  - `spot: { preferred, fallback: on-demand|fail }`
  - `limits`: pool-wide cap (vCPU / GPU count / approximate $), → Karpenter `limits`
  - `scaleToZero` + `consolidateAfter` (CI-tuned, aggressive by default)
  - `warmNodes` (optional pre-warm buffer; default 0 = pure scale-to-zero)
  - `disk`, optional `amiFamily` override
  - auto-taint `flint.dev/ci=<pool>:NoSchedule` for CI isolation (Flint auto-tolerates on
    its Job pods, so pipeline authors never see it)
  - `isolation: shared | node` (future: one-pod-per-node for heavy/sensitive jobs)

The dev-facing profile is **derived** from the managed intent, so a pipeline references a
managed pool exactly like a reference pool.

## Managed-mode reconciliation (the add-on)

For each `mode: managed` pool the controller converges a Karpenter `NodePool` +
`EC2NodeClass`:
- `NodePool.spec.template.spec.requirements` ← arch, instance families/sizes, GPU,
  `karpenter.sh/capacity-type` (spot/on-demand from `spot`)
- `NodePool.spec.template.spec.taints` ← the CI isolation taint
- `NodePool.spec.limits` ← pool cap
- `NodePool.spec.disruption` ← `consolidationPolicy: WhenEmpty`, `consolidateAfter`
  (scale-to-zero is just this config — Karpenter does the work, Flint doesn't implement
  autoscaling)
- `EC2NodeClass` ← subnet/SG/role/AMI/disk from the cluster provisioning profile
- ownership: label `flint.dev/managed-by: <pool>`; GC the NodePool+NodeClass on pool
  delete (with drain); never touch unlabeled NodePools

**Provisioner seam**: a small interface (`Provisioner.Reconcile(pool) / Delete(pool)`)
with an AWS/Karpenter implementation first; Azure reuses the NodePool logic and swaps the
NodeClass renderer; GKE (ComputeClass) is a separate future impl. No engine changes.

**Cost/latency honesty**: scale-to-zero trades a cold start (~30–60s: launch + join +
image pull) for zero idle cost. `warmNodes` is the escape hatch when latency matters; a
later enhancement can pre-warm from queue depth (Flint knows pending runs — value Karpenter
defaults can't give).

## Pipeline UX (the part that matters)

Honest finding: the YAML for this **already exists** — `runner:` and `resources:` at the
job level, and `runner:` (plus `image`/`serviceAccount`) at the pipeline level. The work
is to make the **cascade and semantics crisp, validate against the pool, and surface pools
to authors** — not invent new syntax.

### The cascade (least surprising, k8s-aligned)

Three levels, each more specific winning:
1. **Pipeline default** — `runner:` / `resources:` apply to every job unless overridden.
2. **Job override** — a job picks its pool and (optionally) its pod resources.
3. **Pool default** — if a job sets no `resources:`, it gets the pool's default request.

A job is a pod, so resources are necessarily **per-job (= per-pod)**. Steps inside a job
share the pod and cannot have their own resources — this is stated explicitly so authors
aren't surprised; "per-pod resources" *is* job-level `resources:`.

```yaml
runner: standard            # pipeline default pool (optional)
resources:                  # pipeline default request (optional)
  cpu: "2"
  memory: 4Gi

jobs:
  lint:
    # inherits pool "standard" + default 2cpu/4Gi
    steps:
      - run: npm run lint

  test:
    runner: cpu-large       # override: bigger pool for this job
    resources:              # override: explicit pod request within that pool
      cpu: "8"
      memory: 16Gi
    steps:
      - run: npm test

  train:
    runner: gpu             # GPU pool — pool decides vendor/model (e.g. a100)
    resources:
      gpu: 1                # author says how many; pool says what kind
      memory: 32Gi
    steps:
      - run: python train.py
```

### Semantics (kept simple, with an escape hatch)

- `resources: { cpu, memory, gpu }` is the **pod's request**. Limits default to requests
  (current behavior — the safe CI default that avoids noisy-neighbor). An advanced
  `limits: { cpu, memory }` block is available when you genuinely want request < limit.
- **GPU**: count only at the job level; vendor/model is a *pool* property. `gpu: 2` on a
  pool whose nodes have no GPU is a **compile-time error**, not a stuck pod.
- **No `runner:` anywhere** → the configured cluster **default pool**. Simple pipelines
  ignore pools entirely (progressive disclosure).

### Validation (fail fast — the real UX win)

Because Flint knows each pool's profile (registry/DB), pipeline compile validates:
- referenced pool exists → else error listing valid pools (and a near-match suggestion);
- requested GPU vendor/count is satisfiable by the pool;
- requested cpu/memory fits within the pool's largest possible node (managed: the envelope
  max; reference: the pool's declared node size) → else error, not an unschedulable pod.

This is the concrete advantage of the pool abstraction over raw k8s: bad compute requests
are caught when the pipeline is parsed, with a message that names the fix.

## Dev-facing pool discovery

- `flint runner list` (exists) → name, profile (cpu/mem/gpu/arch), spot, mode.
- Admin → **Runners** UI page: browse pools read-first; create/edit/delete (incl. managed
  intent) for admins. Validation errors in run views link to the pool list.

## Admin UX for managed pools

Admin → Runners → New pool → `mode: managed`:
- pick capacity (instance families *or* cpu/mem ranges), arch, optional GPU type
- spot policy, pool limits (cap), scale-to-zero + `consolidateAfter`, optional warm buffer
- (provisioning profile is configured once at the add-on level, not per pool)
- Save → controller reconciles the Karpenter NodePool+NodeClass; pool shows a **status**
  (Ready / Reconciling / Error: e.g. "storageClass not RWX", "Karpenter not detected",
  "NodeClass IAM invalid") — surfaced from reconcile conditions.

## Validation & failure modes (summary)

- Add-on disabled / Karpenter absent → managed pools rejected at save with a clear reason;
  reference pools unaffected.
- Provisioning profile missing/invalid → managed pool save error.
- Reconcile partial failure (NodePool created, NodeClass IAM wrong → nodes never join) →
  pool status `Error` with the cause; pipelines referencing it warn at compile.
- Pool deleted while in use → block or drain-and-GC (decision below).

## Phasing

1. **Reference mode + default pool, first-class via API/UI** (small; no infra authority).
   Formalize the `"standard"` default into config; pools manageable in the admin UI;
   compile-time validation of `runner:`/`resources:` against pool profiles.
2. **Autoscaling add-on, AWS/Karpenter, managed mode**: provisioner seam +
   NodePool/EC2NodeClass renderer + reconcile/GC + precondition detection + provisioning
   profile config + admin managed-pool UI with status.
3. **Azure** (reuse NodePool, swap NodeClass) and **warm-buffer/pre-warm** enhancements.
4. **GKE ComputeClass** backend and/or a CRD/GitOps adapter — only on real demand.

## Open decisions

- **Pool source of truth**: API/DB canonical (recommended, consistent with current
  direction) vs. RunnerPool CRD for GitOps infra teams. Proposal: API/DB now, CRD adapter
  later if demanded.
- **Delete-in-use**: block deletion of a pool referenced by recent/active runs, or allow +
  drain? Proposal: block on active runs, allow when idle.
- **Limits semantics**: do pool `limits` enforce a hard cap (Karpenter `limits`, may leave
  pods Pending) or just inform? Proposal: hard cap, surfaced in UI, with Pending reason
  shown on stuck steps.
- **Request vs limit default**: keep requests == limits for CI determinism, or expose both
  by default? Proposal: requests==limits default, `limits:` escape hatch.
```
