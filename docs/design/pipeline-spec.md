# Pipeline YAML Specification

> **Status:** canonical. This supersedes the earlier flat-`steps` + auto-coalescing
> model. A pipeline is now **jobs → steps**: a job is a pod, a step is a command
> inside that pod. The old design (every top-level step a potential pod, implicit
> coalescing, per-step workspace sync) is retired.

## Overview

A Flint pipeline is a YAML file under `.flint/` in a repository. It describes a
DAG of **jobs**. Each job runs in its own isolated environment (a Kubernetes pod
for container jobs) with its own base image and its own disk. Jobs hand data to
each other through two explicit channels — `outputs` (values) and `artifacts`
(files). Inside a job, **steps** run sequentially and share the job's disk.

### Design principles

- **Infra-lite, scale-to-zero.** Between runs, only the control plane exists. A
  run brings up exactly the pods it needs and tears them down. The only
  dependency that grows with you is an object store (S3-compatible).
- **The pod boundary is explicit in the YAML.** A job *is* the unit of
  isolation. You read pod boundaries straight off the page — different image or
  different disk needs ⇒ a different job.
- **Cross-pod state has exactly two named doors.** `outputs` and `artifacts`.
  Nothing else crosses a job boundary implicitly.
- **One way to do each thing.** No alternative storage backends or coalescing
  heuristics surfaced to the user.

---

## Mental model

```
pipeline (one .flint/*.yaml file)
└── jobs                         ← each job is a pod (own image + disk + runner)
    └── steps                    ← sequential commands inside that pod, shared disk

within a job   →  steps share one disk; fast; no handoff
across jobs    →  `needs:` + `outputs:` + `artifacts:` (via the object store)
```

A simple pipeline is **one job with several steps** = one pod = no object store,
nothing persistent. Complexity (a second pod, the object store) appears only when
you add a second job.

> **Terminology.** The user-facing language is **pipeline → jobs → steps**. The
> engine underneath is product-neutral and speaks **workflow → waves → steps**; a
> CI *job* compiles to an engine step-group (one pod) and `needs:` compiles to the
> engine's wave/dependency graph. Keep these vocabularies distinct in code and docs.

---

## Minimal examples

```yaml
# .flint/ci.yaml — plain CI, single job, single pod
image: golang:1.26
triggers:
  push:
    branches: [main]
  pull_request:
    branches: [main]
jobs:
  build-test:
    steps:
      - run: go build ./...
      - run: go test ./...
```

```yaml
# .flint/deploy.yaml — multi-job CD with explicit handoff
image: golang:1.26
environments: [staging, production]
triggers:
  push:
    branches: [main]
    environments: [staging]
  promotion:
    from: staging
    environments: [production]
jobs:
  build:
    disk: 20Gi
    steps:
      - run: go build -o bin/app ./...
      - run: echo "version=$(git rev-parse --short HEAD)" >> "$FLINT_OUTPUT"
    outputs:
      version: ${{ steps.outputs.version }}
    artifacts:
      - bin/

  approve:
    needs: [build]
    environments: [production]      # gate only on production promotions
    gate:
      approvers: [role:release-manager]
      minApprovals: 1

  deploy:
    needs: [build, approve]
    image: gcr.io/kaniko-project/executor
    steps:
      - run: /kaniko/executor --destination repo/app:${{ needs.build.outputs.version }}
```

---

## Top-level schema

```yaml
# Optional defaults applied to every job that doesn't override them.
image: golang:1.26              # default base image
runner: standard               # default runner pool
serviceAccount: ci-deployer     # default K8s ServiceAccount

# Optional: restrict which environments this pipeline can target.
environments: [staging, production]

# Required: at least one trigger.
triggers: { ... }

# Required: at least one job.
jobs: { ... }
```

| Key | Required | Meaning |
|-----|----------|---------|
| `image` | no | Default base image for jobs without their own `image`. |
| `runner` | no | Default runner pool. |
| `serviceAccount` | no | Default K8s ServiceAccount for job pods. |
| `environments` | no | If set, the pipeline can only target these environments (the narrowing funnel, below). Default: any. |
| `triggers` | yes | At least one trigger. |
| `jobs` | yes | Map of job-name → job. At least one. |

---

## Jobs

A job is a map keyed by job name. The name is the identity used in `needs:` and in
`needs.<job>.outputs.*`.

```yaml
jobs:
  <job-name>:
    # — Isolation (per-pod) —
    image: node:22               # base image (default: top-level image)
    disk: 20Gi                   # scratch size for this pod (default: sane platform default)
    runner: gpu-pool             # runner pool (default: top-level runner)
    serviceAccount: deployer      # K8s SA override

    # — Graph —
    needs: [build]               # job dependencies (the ONLY cross-pod edges)

    # — Scoping —
    environments: [production]   # run this job only for these target environments
    if: ${{ branch == 'main' }}  # conditional execution (expression)
    when: onSuccess              # onSuccess (default) | onFailure | always

    # — Body (exactly one of: steps | gate) —
    steps: [ ... ]               # sequential commands in the pod
    gate: { ... }                # OR an approval checkpoint (non-pod job)

    # — Handoff out —
    outputs:                     # values exported to downstream jobs
      version: ${{ steps.outputs.version }}
    artifacts:                   # files exported to downstream jobs (that `needs:` this)
      - bin/
      - dist/**/*.js

    # — Optional pod features —
    services:                    # sidecar containers for the job's pod (e.g. a DB)
      - name: postgres
        image: postgres:16
        env: { POSTGRES_PASSWORD: test }
    cache:                       # cross-run dependency cache for this job
      key: deps-${{ hashFiles('go.sum') }}
      paths: [/go/pkg/mod]
    matrix:                      # expand this job into N parallel pods
      go: ["1.25", "1.26"]
    timeout: 30m                 # whole-job timeout (default: 1h)
```

### Job fields

- **`image`** — the pod's base image. One image per job. *If a step needs a
  different image, that is a different job.* (Sidecars are the exception — see
  `services`.)
- **`disk`** — scratch for this pod. Each job pod gets its **own** disposable
  volume sized to this, independent of every other job. Default is a sensible
  platform value; bump it for big builds (image builds, large dependency trees).
- **`needs`** — the job DAG. This is the only place a pod boundary is crossed.
  A job runs once all jobs in `needs` have completed (subject to `when`).
- **`environments`** — restricts the job to the listed target environments. In a
  CD run targeting an environment not in this list, the job is skipped and its
  edges are bypassed.
- **`if` / `when`** — `if` is a boolean expression evaluated against run context
  (including `needs.*.outputs`); `when` controls failure-state behavior
  (`onSuccess` default / `onFailure` / `always`).
- **`steps` | `gate`** — exactly one. `steps` makes it a container (pod) job;
  `gate` makes it a non-pod approval job.
- **`outputs` / `artifacts`** — the two handoff channels (below).
- **`services`** — sidecar containers in the job's pod (databases, emulators).
- **`cache`** — cross-run cache restored at job start, saved at job end.
- **`matrix`** — expands the job into one pod per combination (below).
- **`timeout`** — the whole job's wall-clock budget.

### `needs` semantics

- **Direct-only output scope.** A job may read `${{ needs.<X>.outputs.* }}` only
  if `<X>` is in *its own* `needs`. Transitive dependencies are not in scope — to
  read a job's outputs, list it in `needs` even if you already depend on it
  indirectly. (No spooky-action; explicit, like GitHub Actions.)
- **A skipped need is satisfied, not blocking.** If a needed job is skipped —
  its `if:` was false, or its `environments` excluded the target — downstream
  jobs **still run**; the skipped job is treated as neutral. Only a *failed*
  need blocks downstream jobs, unless the downstream declares `when: onFailure`
  or `when: always`. This is what lets one pipeline serve PR, staging, and
  production from the same graph (e.g. `deploy` runs on staging even though the
  production-only `approve` gate and the conditional `migrate` job are skipped).

### Steps (inside a job)

Steps run **sequentially** in the job's pod, sharing its disk and image. Steps do
not have pods, `needs`, `environments`, or `matrix` — those are job concerns.

```yaml
steps:
  - run: npm ci                  # shell command(s); string or list
  - name: build                  # optional human label
    run:
      - npm run build
      - npm run bundle
    env: { NODE_ENV: production }
    workingDir: ./web
    shell: bash                  # sh (default) | bash | python
    secrets: { NPM_TOKEN: npm-token }   # inject secret as env var
    timeout: 10m                 # per-step timeout
    continueOnError: true        # don't fail the job if this step fails
    retry: { attempts: 3, delay: 5s }
    if: ${{ inputs.run_bundle == 'true' }}
  - use: ecr-login               # reuse a step template (see Reuse)
    with: { registry: ${{ env.ECR_REGISTRY }} }
```

Step fields: `name`, `run` | `use`, `with` (for `use`), `shell`, `workingDir`,
`env`, `secrets`, `timeout`, `continueOnError`, `retry`, `if`, `when`. (`image` is
**not** a step field — image is the job's.)

#### Step outputs within a job

A step writes `key=value` lines to the file at `$FLINT_OUTPUT`. Later steps in the
**same job** read them via `${{ steps.outputs.<key> }}`. To expose a value to
**other jobs**, surface it under the job's `outputs:` map.

---

## Data flow

There are exactly two scopes, and they have different mechanics by design:

### Within a job — implicit, free, fast

Steps share the pod's disk. Files written by step 1 are present for step 2. Step
outputs flow via `$FLINT_OUTPUT` → `steps.outputs.*`. No declaration, no transfer.

### Across jobs — explicit, via the object store

Two channels, both declared at the job level:

1. **`outputs`** (small values). The producing job declares
   `outputs: { version: ${{ steps.outputs.version }} }`; a consumer with
   `needs: [build]` reads `${{ needs.build.outputs.version }}`. Resolved when the
   consumer pod is dispatched — safe, because the producer is fully done by then.
   This is the clean place output→command interpolation happens.
2. **`artifacts`** (files). The producing job declares `artifacts: [bin/]`; any
   job that `needs:` it has those artifacts materialized into its workspace at
   start. Stored as one compressed object per job in the object store.

### Source / checkout

**Each job checks out the source independently** (auto-clone into every container
job's workspace at start, unless the job opts out). Source is *not* implicitly
shared across jobs — that would re-introduce hidden cross-pod state. Re-checkout
is cheap (shallow/partial) and cacheable; large trees that must be reused can be
passed as an artifact instead.

---

## Execution model

This is how the pipeline maps to pods, disk, and the agent. It is part of the
canonical contract — the YAML model and the execution model are co-designed.

### Jobs are pods; steps are in-pod

A container job becomes **one Kubernetes pod** that runs its steps in sequence.
`needs:` becomes the engine's dependency graph; independent jobs run as parallel
pods. Gate and HTTP jobs are **not pods** — they execute in the control plane
(an approval wait, an in-process HTTP call). Precisely:

> A job is the unit of isolation. Container jobs are pods; gate/http jobs run in
> the control plane.

### The agent: a per-job sidecar (not an entrypoint, not per-run)

Each container job is **one pod with three parts**:

```
JOB POD   (one per job — not per step)
├── initContainer  (flint agent image)
│     • checkout source + download `needs` artifacts + restore cache → shared volume
│     • copy a static busybox into the shared volume (gives any image a shell)
│
├── container: <the job's image>          ← runs the steps; holds NO credentials
│     • command: /flint/busybox sh -ec '<steps, each wrapped with markers>'
│     • reads/writes the shared workspace volume
│     • emits per-step log / exit / $FLINT_OUTPUT markers to the shared volume
│
└── sidecar  (native sidecar, flint agent image)   ← the job's "brain"
      • owns credentials (registry push tokens, run/task token, object-store creds)
      • ships logs to the log sink in real time
      • observes step markers/exit via the shared volume
      • on completion: push declared artifacts + outputs + save cache to object store
```

**Why a sidecar (and not the alternatives):**

- **Sidecar, not entrypoint.** The agent runs in its **own container**, isolated
  from the step code, so orchestration credentials live only in the sidecar and
  never enter the user container. This is the safe default for an OSS CI that will
  run **untrusted / fork-PR code**. The old "a sidecar per *step* multiplies"
  objection is gone — under the jobs model this is **one sidecar per job**, so a
  pipeline has a handful, not dozens.
- **Not a single per-run agent.** A separate per-run pod can't see a job pod's
  disk (the pod boundary) and would be a per-run single point of failure; the
  shared channel between jobs is the object store, not a shared pod.
- **Completion is observed control-plane.** The worker watches the K8s Job for
  terminal state; the sidecar's push lands outputs in the object store, which the
  worker reads to advance the DAG. No inbound `/complete` callback from the pod.

**Image contract:** job images must be **Linux** and match the cluster's **CPU
arch**. `run:` steps get a shell on any image — including `distroless`/`scratch`
— via the injected static busybox; the user's own tools must still exist in the
image.

**Requirement:** Kubernetes **≥ 1.28** (native sidecar containers). The agent
sidecar and `services:` sidecars (databases, emulators) use the same pod-sidecar
mechanism.

### Scratch: per-job disposable disk

Each job pod gets its own scratch sized by `disk:`, shared by the pod's containers
(init, user, sidecar) and independent of other jobs:

- Default / small: a node-backed `emptyDir` with an `ephemeral-storage` request so
  the scheduler accounts for it. Instant, free, capped by node disk.
- Large (declared `disk:` above a threshold — e.g. image builds): a **generic
  ephemeral volume** (`volumeClaimTemplate`) — a fresh RWO block volume created
  *with* the pod and deleted *with* it. Sized, safe (no node disk-pressure), still
  ephemeral and scale-to-zero. RWO suffices because only this one pod mounts it.
  ~10–30s attach is paid once per job (parallel jobs attach concurrently).

### Handoff: the object store

Artifacts, job outputs, and cross-run cache live in an S3-compatible object store
(one compressed object per job for artifacts). A purely sequential single-job
pipeline never touches it. Same-region transfer is free; one-object-per-job keeps
request counts low.

### Footprint summary

| Pipeline shape | Pods spun up | Persistent infra |
|---|---|---|
| Single job (sequential) | 1 pod (init + user + agent sidecar) + its scratch | none — no object store |
| Multiple jobs / parallel | one pod per job + scratch each | object store only |
| Gate / HTTP jobs | none (control plane) | none |

Between runs: **zero** Flint workload pods. Setup floor: a default StorageClass
(present on every managed K8s) plus, once you use artifacts/parallelism/cache, an
object-store bucket. Local/dev (Podman) maps scratch to a host temp dir and
handoff to a host dir — none of the cluster machinery applies.

---

## Triggers

At least one trigger is required. Multiple may coexist.

```yaml
triggers:
  push:
    branches: [main, "release/*"]
    paths: ["src/**"]            # optional path filter
    environments: [staging]      # optional (CD)
  pull_request:
    branches: [main]
    paths: ["src/**"]
    # pull_request can NEVER have environments — always plain CI
  manual:
    environments: [staging, production]
    inputs:
      - { name: reason, type: string, required: true }
      - { name: dry_run, type: boolean, default: "false" }
      - { name: region, type: choice, options: [us, eu], default: us }
  schedule:
    cron: "0 2 * * *"
    environments: [staging]
  tag:
    patterns: ["v*"]
    environments: [production]
  promotion:
    from: staging                # run after a staging run succeeds
    environments: [production]
    requireStatus: succeeded     # default
  webhook:
    secret: ${{ secrets.hook_secret }}
    environments: [staging]
```

### Trigger compatibility

Multiple triggers can coexist. Rules:

1. **`pull_request` cannot have `environments`** — it is always plain CI.
2. **`manual` inputs + any automated trigger** ⇒ every input must have a
   `default` (automated triggers can't prompt).
3. **`promotion` requires `environments` and `from`**, and `from` cannot overlap
   the target `environments`.
4. **No duplicate triggers of the same type**, except `promotion` (multiple
   allowed with different `from`).
5. **Environment consistency** — if top-level `environments` is set, every
   trigger's environment references must be a subset.

---

## Environments (CD)

A pipeline is **environment-aware** if any of: a top-level `environments` key; any
trigger or job has `environments`; or any expression references environment-scoped
`${{ secrets.* }}`, `${{ env.* }}`, or `$FLINT_ENVIRONMENT`. This is validated
statically — an environment-aware pipeline whose automated triggers omit
`environments` fails validation.

### The narrowing funnel

```
top-level environments      → pipeline may target these       (default: any)
  └─ trigger environments    → this trigger creates runs for these
      └─ job environments     → this job runs only in these
```

Each level must be a subset of the one above. Environment scoping is a **job**
property — there is no step-level environment filtering.

### Resolution at runtime

1. Determine the target environment (trigger config or manual selection).
2. Jobs whose `environments` exclude the target are **skipped**; their `needs`
   edges are bypassed.
3. Env vars and secrets resolve for the target environment.
4. `$FLINT_ENVIRONMENT` is set to the environment slug.
5. Gate jobs for the target environment are evaluated.

---

## Gates

A gate is a **job** with a `gate:` body instead of `steps:`. It runs in the
control plane (no pod) and pauses the DAG until approvals arrive.

```yaml
jobs:
  approve:
    needs: [build]
    environments: [production]
    gate:
      approvers: [role:release-manager, team:platform, user:alice]
      minApprovals: 2            # default: 1
```

Approver syntax: `role:<name>`, `team:<name>`, or `user:<id>`. Downstream jobs
`needs: [approve]` wait for the gate to clear.

---

## Reuse

Steps may pull in shared step templates with `use:` (local file, cross-repo file,
or a registered step template). Reuse is at the **step** level, inside a job.

```yaml
jobs:
  build:
    steps:
      - use: ./fragments/setup.yaml   # inlines the template's steps here, in order
      - run: npm run build
      - use: ecr-login                # a registered single-step template
        with: { registry: ${{ env.ECR_REGISTRY }} }
```

**The black-box rule:** a `use:` block is opaque. Since steps within a job are
sequential, a template simply expands its steps in place — you never reference a
template's internal step names. If the template author renames internals, nothing
breaks.

---

## Expressions

`${{ ... }}` expressions are evaluated against a typed context. Evaluation is
sandboxed with a time bound.

### Context

| Namespace | Available |
|-----------|-----------|
| `branch`, `commitSha`, `shortSha`, `tag` | git context |
| `triggeredBy`, `triggerType`, `status` | run context |
| `environment` | target environment slug (CD) |
| `project`, `run` | identifiers |
| `inputs.*` | manual-trigger inputs |
| `env.*`, `secrets.*` | environment variables / secrets |
| `matrix.*` | this job's matrix combination |
| `needs.<job>.outputs.*` | a dependency job's outputs (cross-job dataflow) |
| `steps.outputs.*` | earlier steps' outputs (within the same job) |
| `webhook.*` | webhook trigger payload |

### Functions & operators

- Functions: `hashFiles(glob)`, `contains(haystack, needle)`,
  `startsWith(s, prefix)`.
- Operators (for `if`): `==`, `!=`, `&&`, `||`, `!`, comparisons, parentheses.

User-chosen names (manual input names, matrix keys) that collide with a reserved
context name are flagged by validation.

---

## Matrix

`matrix` is a **job** property. It expands the job into one pod per combination,
running in parallel. `${{ matrix.<key> }}` is available in the job's steps, image,
env, and cache key.

```yaml
jobs:
  test:
    matrix:
      go: ["1.25", "1.26"]
      os: [linux, darwin]
    image: golang:${{ matrix.go }}
    steps:
      - run: GOOS=${{ matrix.os }} go test ./...
```

Downstream `needs: [test]` waits for **all** matrix variants.

---

## Validation

`flint validate` runs statically (no cluster, no fetch):

- Exactly one of `steps` / `gate` per job; at least one job; at least one trigger.
- `needs` references exist; the job graph is acyclic.
- Environment narrowing subset rules; environment-awareness consistency.
- Trigger compatibility rules.
- Expression parse + reserved-name collision checks.
- `outputs` reference existing step outputs; `artifacts` globs are well-formed.
- One image per job; `disk` parses as a quantity; durations parse.

`flint simulate --environment <env>` previews which jobs run/skip and how
environments/secrets resolve, on demand. Validation never caches — always fresh.

---

## Worked example

A single pipeline exercising matrix, parallel/sequential jobs, cross-job
outputs + artifacts, conditional jobs (on inputs and on an upstream output),
gates, environments + promotion, service sidecars, cache, per-job disk, secrets,
retry/timeout/continueOnError, and step-template reuse lives at
[`examples/release.yaml`](examples/release.yaml). Its job graph:

```
        external inputs:  image_tag · skip_tests · run_load_test
                                    │
                                    ▼
                          ┌──────────────────┐
                          │       build       │  root · cache · disk 10Gi
                          │ compile · version │
                          │ detect-migrations │
                          └─────────┬────────┘
              outputs: version, has_migrations   ·   artifacts: bin/
    ┌───────────────┬───────────────────────┬────────────────────────┐
    ▼               ▼                        ▼                         ▼
┌────────┐   ┌────────────┐        ┌──────────────────┐        ┌────────────┐
│  lint  │   │ unit-test  │        │ integration-test │        │  migrate   │ [CD]
│        │   │ ⫶ matrix×3 │        │ + postgres svc   │        │ ⊘ if has_  │
│ (advis)│   │ ⊘ !skip    │        │ ⊘ !skip          │        │ migrations │
└───┬────┘   └─────┬──────┘        └────────┬─────────┘        └─────┬──────┘
    └───────────────┴───────────┬───────────┘                       │
                                ▼   (needs build + all three)        │
                        ┌──────────────────┐                         │
                        │      image        │ [CD] kaniko · disk 50Gi │
                        └─────────┬────────┘                         │
                                  ▼                                   │
                        ┌──────────────────┐                         │
                        │    approve   ◇    │ [CD · prod] gate ·2 appr│
                        └─────────┬────────┘                         │
                                  ▼                                   │
                        ┌──────────────────┐ ◄───────────────────────┘
                        │      deploy       │ [CD]  needs build+image+
                        │ kubectl·SA·secret │        migrate+approve
                        └─────────┬────────┘
                    ┌─────────────┴─────────────┐
                    ▼                            ▼
            ┌──────────────┐            ┌──────────────────┐
            │  load-test   │ [CD]       │     notify        │ [CD]
            │ ⊘ run_load_  │            │ when: always      │
            │   test       │            │ use: slack-notify │
            └──────────────┘            └──────────────────┘

  ⊘ conditional (if:)   ⫶ matrix → N pods   ◇ gate   [CD] has environments (skipped on PRs)
  fan-out below build = parallel jobs; downward chains = sequential.
  skipped need = satisfied → deploy runs on staging even with approve/migrate skipped.
```

By trigger: a **PR** runs `build → lint · unit-test · integration-test` only
(every `[CD]` job is skipped); **push→staging** adds `image → deploy → notify`
(+`migrate` if migrations); **promotion→production** adds the `approve` gate.

---

## What changed from the previous spec

- Flat `steps:` with auto-coalescing and per-step pods ⇒ **`jobs:` (pods) →
  `steps:` (in-pod commands)**. Pod boundaries are explicit, not inferred.
- Step-level `dependsOn`, `environments`, and `matrix` ⇒ **job-level** (`needs`,
  `environments`, `matrix`). Steps are purely sequential within a pod.
- Gates move from step-level to **gate jobs**.
- Cross-pod data is exactly `outputs` + `artifacts` at the job level; within a job
  it's the shared disk. No per-step workspace sync.
- Execution: **per-job agent sidecar** (credentials isolated from step code; native
  sidecar, K8s ≥ 1.28), per-job ephemeral scratch volume, object-store handoff,
  control-plane completion. Job images must be Linux + matching arch; `run:` gets a
  shell on any image via an injected static busybox.
