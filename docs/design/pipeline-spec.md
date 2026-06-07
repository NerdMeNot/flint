# Pipeline YAML Specification

> **Status:** canonical. A pipeline is **jobs → steps**: a job is a pod, a step is
> a command inside that pod. Supersedes the earlier flat-`steps` + auto-coalescing
> model.

## Overview

A Flint pipeline is a YAML file under `.flint/` in a repository. It describes a
DAG of **jobs**. Each job runs in its own isolated environment (a Kubernetes pod
for container jobs) with its own base image and its own disk. Jobs hand data to
each other through two explicit channels — `outputs` (values) and `artifacts`
(files). Inside a job, **steps** run sequentially and share the job's disk.

### Design principles

- **Infra-lite, scale-to-zero.** Between runs, only the control plane exists. A
  run brings up exactly the pods it needs and tears them down. The only growing
  dependency is an object store (S3-compatible).
- **The pod boundary is explicit in the YAML.** A job *is* the unit of isolation
  — different image or disk needs ⇒ a different job.
- **Cross-pod state has exactly two named doors:** `outputs` and `artifacts`.
- **One way to do each thing.** No alternative storage backends, coalescing
  heuristics, or redundant conditional mechanisms surfaced to the user.

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
nothing persistent. Complexity appears only when you add a second job.

> **Terminology.** User-facing: **pipeline → jobs → steps**. The engine underneath
> is product-neutral and speaks **workflow → waves → steps**; a CI *job* compiles
> to an engine step-group (one pod) and `needs:` to the engine's dependency graph.

---

## Minimal example

```yaml
# .flint/ci.yaml — plain CI, single job, single pod
image: golang:1.26
triggers:
  push: { branches: [main] }
  pull_request: { branches: [main] }
jobs:
  build-test:
    steps:
      - run: go build ./...
      - run: go test ./...
```

A larger, full-lifecycle example lives at
[`examples/release.yaml`](examples/release.yaml) (see [Worked example](#worked-example)).

---

## Top-level schema

```yaml
# Defaults applied to every job that doesn't override them.
image: golang:1.26
runner: standard
serviceAccount: ci-deployer

env:                            # pipeline-wide env (merged into every step)
  GOFLAGS: -mod=readonly
secrets:                        # pipeline-wide secrets (see Secrets)
  - { name: registry-creds, env: REGISTRY_AUTH }

environments: [staging, production]   # restrict targetable environments (optional)

concurrency:                    # run-level concurrency (optional, see Concurrency)
  group: ${{ project }}-${{ branch }}
  cancelInProgress: true

triggers: { ... }               # required: at least one
jobs: { ... }                   # required: at least one
```

| Key | Required | Meaning |
|-----|----------|---------|
| `image`, `runner`, `serviceAccount` | no | Defaults for jobs. |
| `env`, `secrets` | no | Pipeline-wide env / secrets (merged into all jobs; see those sections). |
| `environments` | no | If set, the pipeline can only target these. Default: any. |
| `concurrency` | no | Run-level concurrency group. |
| `triggers` | yes | At least one. |
| `jobs` | yes | Map of job-name → job. At least one. |

---

## Jobs

```yaml
jobs:
  <job-name>:
    # — Isolation (per-pod) —
    image: node:22               # base image (default: top-level image)
    disk: 20Gi                   # this pod's scratch (default: platform default)
    runner: gpu-pool
    serviceAccount: deployer

    # — Graph —
    needs: [build]               # job dependencies (the only cross-pod edges)

    # — Scoping / conditions —
    environments: [production]   # run only for these target environments
    if: ${{ branch == 'main' }}  # condition (expression + status functions)

    # — Body (exactly one of: steps | gate) —
    steps: [ ... ]
    gate: { ... }

    # — Env / secrets (merged with pipeline-level) —
    env: { LOG_LEVEL: debug }
    secrets:
      - { name: db-password, env: DB_PASSWORD }

    # — Handoff out —
    outputs: { version: ${{ steps.outputs.version }} }
    artifacts: [bin/, dist/**/*.js]

    # — Pod features —
    services:
      - { name: postgres, image: postgres:16, env: { POSTGRES_PASSWORD: test } }
    cache:
      key: deps-${{ hashFiles('go.sum') }}
      restoreKeys: [deps-]       # partial-hit fallbacks, tried in order
      paths: [/go/pkg/mod]

    # — Matrix / fan-out —
    matrix: { go: ["1.25", "1.26"] }
    failFast: true               # cancel sibling variants on first failure (default true)
    maxParallel: 2               # cap concurrent variants

    # — Limits —
    timeout: 30m                 # whole-job budget (default 1h)
    concurrency: { group: deploy-${{ environment }}, cancelInProgress: false }
```

Key job rules:

- **`image`** — one image per job. *If a step needs a different image, that's a
  different job.* (`services` sidecars are the exception.)
- **`disk`** — this pod's scratch, independent of every other job.
- **`steps` | `gate`** — exactly one. `steps` ⇒ a container (pod) job; `gate` ⇒ a
  non-pod approval job.

### `needs` semantics

- **Direct-only output scope.** A job may read `${{ needs.<X>.outputs.* }}` only
  if `<X>` is in its own `needs` — list it explicitly even if you depend on it
  indirectly.
- **A skipped need is satisfied, not blocking.** If a needed job is skipped (its
  `if:` was false, or `environments` excluded the target), downstream jobs still
  run. Only a *failed* need blocks downstream jobs — and you can still run on
  failure with `if: ${{ failure() }}` / `${{ always() }}`. This lets one pipeline
  serve PR, staging, and production from the same graph.

### Conditions: `if:` + status functions

A job (or step) runs when its `if:` evaluates true. **Default `if:` is
`success()`** — run only if all dependencies (for a job) / prior steps (for a
step) succeeded and the run wasn't cancelled.

Status functions, available in any `if:`:

| Function | True when |
|---|---|
| `success()` | nothing it depends on failed (the implicit default) |
| `failure()` | at least one dependency/prior step failed |
| `cancelled()` | the run was cancelled |
| `always()` | always — runs even on failure/cancel |

Plus `${{ needs.<job>.result }}` (`success` \| `failure` \| `skipped` \|
`cancelled`) to target a specific upstream:

```yaml
rollback:
  needs: [deploy]
  if: ${{ failure() }}                    # or: needs.deploy.result == 'failure'
  steps:
    - run: kubectl -n ${{ environment }} rollout undo deploy/orders-api
```

> There is no `when:` field. `if:` + status functions is the single conditional
> mechanism (this replaces the old `when: onSuccess|onFailure|always`).

### Steps (inside a job)

Steps run **sequentially** in the job's pod, sharing its disk and image. Steps
have no pods, `needs`, `environments`, or `matrix` — those are job concerns.

```yaml
steps:
  - run: npm ci
  - name: build
    run: [npm run build, npm run bundle]
    env: { NODE_ENV: production }
    secrets: [{ name: npm-token, env: NPM_TOKEN }]
    workingDir: ./web
    shell: bash                  # sh (default) | bash | python
    timeout: 10m
    continueOnError: true        # don't fail the job if this step fails
    retry: { attempts: 3, delay: 5s }
    if: ${{ inputs.run_bundle == 'true' }}
  - use: ecr-login               # reuse a step template
    with: { registry: ${{ env.ECR_REGISTRY }} }
```

Step fields: `name`, `run` | `use`, `with`, `env`, `secrets`, `shell`,
`workingDir`, `timeout`, `continueOnError`, `retry`, `if`. (`image` is the job's.)

#### Step outputs within a job

A step writes `key=value` lines to `$FLINT_OUTPUT`; later steps in the **same
job** read `${{ steps.outputs.<key> }}`. To expose a value to other jobs, surface
it under the job's `outputs:`.

---

## Environment variables

`env:` (name → value, plain or `${{ }}`) may be set at **pipeline, job, and step**
level. Merge order is **pipeline < job < step** — the narrower scope wins. `env`
is for non-sensitive values; sensitive values go through `secrets` (brokered and
masked).

---

## Secrets

Secrets are a first-class subsystem. They are resolved by the **agent sidecar**
(which alone holds provider credentials), delivered to the step as **environment
variables or files**, kept on a **tmpfs** (never written to scratch, artifacts, or
the object store), and **masked in logs**. They may be declared at **pipeline,
job, or step** level (merged; narrower wins — prefer the narrowest scope).

A secret binding has a **source** and a **target**:

```yaml
secrets:
  # built-in store (default source), auto-scoped to the run's environment, as env var
  - { name: npm-token, env: NPM_TOKEN }

  # built-in store, mounted as a FILE (for tools that read files)
  - { name: kubeconfig, file: ~/.kube/config, mode: "0400" }

  # external provider, mounted as a file
  - name: gcp-deployer
    from: gcp-sm:projects/acme/secrets/deployer/versions/latest
    file: /secrets/gcp.json
    mode: "0400"

  # external provider (Vault), as env var
  - { name: db-password, from: "vault:secret/data/orders/db#password", env: DB_PASSWORD }
```

**Source** (where the value comes from):

- `name:` alone → Flint's **built-in store** (envelope-encrypted), resolved for the
  run's target **environment** automatically (the staging vs production value).
- `from: <provider>:<ref>` → an **external provider** configured per org by an
  admin; the pipeline only names it. Providers:
  - `vault:` (HashiCorp Vault), `aws-sm:` (AWS Secrets Manager),
    `gcp-sm:` (GCP Secret Manager), `azure-kv:` (Azure Key Vault),
    `k8s:` (a Kubernetes Secret in the run namespace).
  - Provider endpoints/auth live in platform config, never in the pipeline.
    `${{ environment }}` may appear in `<ref>` for per-env paths.

**Target** (exactly one per binding):

- `env: NAME` → injected into the step process by the sidecar. *Not* placed in the
  pod spec, so it never appears in `kubectl describe`/the API.
- `file: PATH` (+ optional `mode:`) → written to a tmpfs file for tools that read
  files (kubeconfig, cloud SA JSON, TLS certs, `.npmrc`, Docker config).

**Levels & masking.** Pipeline secrets apply to all jobs; job secrets to all its
steps; step secrets to that step. The sidecar scrubs known secret values from the
log stream regardless of how they're surfaced.

> This is why the agent is a **sidecar**: provider credentials and resolved secret
> material live only in the sidecar's container, never in the user (step)
> container — the safe default for running untrusted / fork-PR code.

---

## Concurrency

Bound how many runs/jobs in the same logical group run at once.

```yaml
concurrency:
  group: ${{ project }}-${{ branch }}
  cancelInProgress: true        # cancel an in-flight member of this group
```

- **`cancelInProgress: true`** — a newer run cancels the running one in the same
  group. Typical for PR pushes (cancel superseded runs → save spend).
- **`cancelInProgress: false`** — newer runs queue behind the current one.
  Typical for serializing deploys.

Allowed at **pipeline** level (the whole run) and **job** level (so one pipeline
can both cancel superseded PR runs *and* serialize prod deploys):

```yaml
deploy:
  concurrency: { group: deploy-${{ environment }}, cancelInProgress: false }
```

---

## Data flow

### Within a job — implicit, free, fast

Steps share the pod's disk; files from step 1 are present for step 2. Outputs flow
via `$FLINT_OUTPUT` → `steps.outputs.*`.

### Across jobs — explicit, via the object store

1. **`outputs`** (values). Producer declares `outputs: { version: ... }`; a
   consumer with `needs: [build]` reads `${{ needs.build.outputs.version }}`.
   Resolved at consumer dispatch (producer is done) — the clean place
   output→command interpolation happens.
2. **`artifacts`** (files). Producer declares `artifacts: [bin/]`; any job that
   `needs:` it has them materialized at start. One compressed object per job.

### Source / checkout

**Each job checks out source independently** (auto-clone into every container job).
Source is not implicitly shared — that would be hidden cross-pod state. Re-checkout
is cheap/cacheable; large reused trees go through an artifact.

---

## Execution model

The YAML model and the execution model are co-designed; this is part of the
canonical contract.

### Jobs are pods; steps are in-pod

A container job is **one pod** running its steps in sequence; `needs:` is the
dependency graph; independent jobs are parallel pods. Gate and HTTP jobs are
**not pods** — they run in the control plane.

> A job is the unit of isolation. Container jobs are pods; gate/http jobs run in
> the control plane.

### The agent: a per-job sidecar

Each container job is **one pod with three parts**:

```
JOB POD   (one per job — not per step)
├── initContainer (flint agent image)
│     • checkout + download `needs` artifacts + restore cache → shared volume
│     • copy a static busybox into the shared volume (a shell for any image)
├── container: <job image>          ← runs the steps; holds NO credentials
│     • runs each step; emits per-step log/exit/$FLINT_OUTPUT markers
└── sidecar (native sidecar, flint agent image)   ← the job's "brain"
      • SECRET BROKER: fetches from the built-in store / external providers,
        exposes to the step as env or tmpfs files, masks values in logs
      • ships logs to the log sink
      • on completion: push artifacts + outputs + save cache
```

- **Sidecar, not entrypoint, not per-run.** Credentials (secret-provider creds,
  push tokens, run token) live only in the sidecar, isolated from step code — the
  safe default for untrusted/fork-PR CI. One sidecar per *job* (not per step).
- **Completion is observed control-plane:** the worker watches the K8s Job; no
  `/complete` callback.
- **Per-step fidelity via markers.** Because steps share one pod, the agent emits
  structured boundary markers (step name, start, end, exit, duration) into the log
  stream, so the UI renders **collapsible per-step sections** with status/timing.
- **Image contract:** job images must be **Linux** and match the cluster **CPU
  arch**. `run:` gets a shell on any image (incl. distroless/scratch) via the
  injected static busybox; the user's own tools must exist in the image.
- **Requires K8s ≥ 1.28** (native sidecars). `services:` sidecars use the same
  mechanism.

### Scratch: per-job disposable disk

Sized by `disk:`, shared by the pod's containers, independent per job:

- Default/small: node `emptyDir` + `ephemeral-storage` request. Instant, free,
  node-capped.
- Large: **generic ephemeral volume** (`volumeClaimTemplate`) — a fresh RWO block
  volume created/deleted with the pod. Sized, safe, still scale-to-zero. RWO
  suffices (one pod). ~10–30s attach once per job (parallel jobs attach
  concurrently).

### Handoff & footprint

Artifacts/outputs/cache live in an S3-compatible object store (one object per job
for artifacts). A sequential single-job pipeline never touches it. Between runs:
**zero** Flint workload pods. Setup floor: a default StorageClass + (once you use
artifacts/parallelism/cache) a bucket.

---

## Triggers

```yaml
triggers:
  push:
    branches: [main, "release/*"]
    paths: ["src/**"]
    environments: [staging]
  pull_request:                    # never has environments — always plain CI
    branches: [main]
    paths: ["src/**"]
  manual:
    environments: [staging, production]
    inputs:
      - { name: reason, type: string, required: true }
      - { name: dry_run, type: boolean, default: "false" }
      - { name: region, type: choice, options: [us, eu], default: us }
  schedule: { cron: "0 2 * * *", environments: [staging] }   # UTC
  tag: { patterns: ["v*"], environments: [production] }
  promotion: { from: staging, environments: [production], requireStatus: succeeded }
  webhook: { secret: ${{ secrets.hook_secret }}, environments: [staging] }
```

Compatibility rules:

1. `pull_request` cannot have `environments` — always plain CI.
2. `manual` inputs + any automated trigger ⇒ every input must have a `default`.
3. `promotion` requires `environments` and `from`; `from` can't overlap the target.
4. No duplicate triggers of a type, except `promotion` (multiple with distinct `from`).
5. If top-level `environments` is set, every trigger's references must be a subset.

---

## Environments (CD)

A pipeline is **environment-aware** if any of: a top-level `environments` key; any
trigger/job has `environments`; or any expression references env-scoped
`${{ secrets.* }}`, `${{ env.* }}`, or `$FLINT_ENVIRONMENT`. Validated statically.

### Narrowing funnel

```
top-level environments  → trigger environments  → job environments
```

Each level must be a subset of the one above. Environment scoping is a **job**
property (no step-level filtering).

### Runtime resolution

1. Determine the target environment (trigger config or manual selection).
2. Jobs whose `environments` exclude the target are skipped; `needs` edges bypass.
3. Env/secrets resolve for the target; `$FLINT_ENVIRONMENT` is set.
4. Gate jobs for the target are evaluated.

---

## Gates

A gate is a **job** with a `gate:` body (non-pod, control plane):

```yaml
approve:
  needs: [image]
  environments: [production]
  gate: { approvers: [role:release-manager, team:platform], minApprovals: 2 }
```

Approver syntax: `role:<name>`, `team:<name>`, `user:<id>`.

---

## Reuse: modules

Reuse goes through **one keyword, `use:`** (and `extends:` for whole-pipeline
reuse). The unit of reuse is a **module**: a versioned, parameterized thing with
typed inputs/outputs and a declared *environment stance*. There are four kinds,
organized around the one axis that actually matters — **does the unit bring its
own environment, or run in the caller's?**

| `kind` | Stance | Used at | Environment |
|---|---|---|---|
| `action` | **containerized** — brings its own image | a step | portable anywhere (runs in its own image) |
| `steps` | **inline** — expands into the caller | a step position | runs in the caller's image → **must declare `requires:`** |
| `job` | **inline** — expands into a job | a job (`use:`) | pins its own image → env-honest by construction |
| `pipeline` | **inline** — the whole DAG | a pipeline (`extends:`) | composes jobs |

**Composition is by parameterization, never by merge.** The **black-box rule**: a
module is opaque — you pass `inputs` (and, for the steps-hole, a block of steps);
you never reach inside or override its keys. No deep-merge, no internal-name
references. This deletes the biggest reuse footgun (GitLab/Azure-style override
surprises) by construction.

### Module definition

```yaml
name: <module-name>
kind: action | steps | job | pipeline
inputs:
  some_str: { type: string, required: true }
  count:    { type: number, default: 1 }
  mode:     { type: enum, options: [a, b], default: a }
  steps:    { type: steps }                 # the "hole" (job/steps kinds)
outputs:
  version:  { type: string, value: ${{ steps.outputs.version }} }
requires:                                   # env contract (inline kinds that run in caller's image)
  family: debian                            # debian | rhel | alpine
  tools: [node>=18]
# ── body, by kind ──
run: { image: ... }                         # kind: action  (brings its image)
steps: [ ... ]                              # kind: steps
job: { image: ..., steps: [ ... ] }         # kind: job
jobs: { ... }                               # kind: pipeline
```

### Input types

`string`, `number`, `boolean`, `enum` (with `options`), and **`steps`** — a block
of steps the caller supplies, dropped in with `inject:`:

```yaml
# in a job/steps module
steps:
  - run: setup
  - inject: ${{ inputs.steps }}             # caller's steps run here, in this image
  - run: teardown
```

The `steps` hole covers most real reuse ("wrap my steps in standard setup/
teardown") without inheritance.

### Environment contracts (static compatibility)

Inline modules that run in the caller's image (`kind: steps`, and any inline body
that doesn't set its own `image`) **must declare `requires:`**. `flint validate`
checks it against the consuming job's image and **fails before the run** on a
mismatch:

```
✖ job "tools" → module "yum-install@1" requires { family: rhel },
    but job image "ubuntu:24.04" is { family: debian }
```

This eliminates the classic footgun (a `yum` step used on Ubuntu) at authoring
time. `action` and `job` modules bring/own their image, so they're env-honest and
skip the check.

### References are immutable — there is no lockfile

A reference pins to something that **cannot change underneath you**, so no lockfile
is needed:

- **Exact version:** `use: go-ci@2.3.1` — registry versions are immutable and
  content-addressed. The manifest *is* the pin; the bump shows in the PR diff.
- **Publisher alias:** `use: go-ci@stable` — a moving pointer only the *publisher*
  (e.g. the platform team) can repoint, for intentional central propagation
  (push a security patch to all consumers at once).
- **Local:** `use: ./templates/x.yaml` — pinned by the repo commit.
- **Cross-repo:** a tag or commit SHA; floating branches are flagged.

**Reproducibility and integrity come from the platform, not a file:** the registry
guarantees a version is immutable, and every run records the exact module versions
it used (server-side). So "re-run identically" works with zero repo artifacts, and
there is nothing to maintain, conflict on, or forget to commit. Non-determinism
exists only where someone explicitly opts into an alias.

### Where modules live (registry, multi-fed)

The **registry** is the source of truth, populated however a team prefers:

- **GitOps (recommended for shared modules):** a repo whose modules publish to the
  registry on tag — versioned, reviewed, immutable.
- **CLI / API / UI:** `flint module publish ./modules/go-ci.yaml --version 2.3.1`.
- **Local files:** `use: ./...` — no registry at all, zero ceremony, in-repo.

No dedicated `.github`-style repo is required: modules can live in the app repo,
a shared repo, or nowhere (published directly).

### Safety: two layers

1. **Pre-run, surfaced in the UI / as a PR check:** unknown/yanked/deprecated
   version, env-compat mismatch, input/output type mismatch, and "a newer version
   is available." You see breakage *before* merging.
2. **Runtime, as the backstop:** anything that slips through fails the run with a
   precise message; you fix-forward (edit the pin).

### Consuming modules

```yaml
# step-level Action (containerized) and step template (inline)
jobs:
  ci:
    use: go-service-ci@^?  # NO — ranges aren't allowed; pin exact or an alias
  build:
    use: go-service-ci@2.3.1        # job module + steps-hole
    with:
      steps:
        - run: go build ./...
  scan:
    needs: [build]
    steps:
      - use: trivy-scan@1.6.0       # step Action (its own image)
        with: { image: "app:${{ needs.build.outputs.version }}" }
  tools:
    image: ubuntu:24.04
    steps:
      - use: apt-install@1.2.0      # step template; requires:debian ✓ on ubuntu
        with: { packages: "jq curl" }
```

```yaml
# whole-pipeline reuse
extends: go-service-pipeline@2.1.0
with: { service: orders-api, deploy_target: orders }
triggers: { push: { branches: [main] } }   # consumer supplies triggers
```

Worked module + consumer files: [`examples/modules/`](examples/modules/) and
[`examples/orders-api.ci.yaml`](examples/orders-api.ci.yaml) /
[`examples/payments-api.ci.yaml`](examples/payments-api.ci.yaml).

---

## Expressions

`${{ ... }}` is evaluated against a typed, sandboxed context.

### Context

| Namespace | Available |
|---|---|
| `branch`, `commitSha`, `shortSha`, `tag` | git context |
| `triggeredBy`, `triggerType`, `status` | run context |
| `environment` | target environment slug (CD) |
| `project`, `run` | identifiers |
| `inputs.*` | manual-trigger inputs |
| `env.*`, `secrets.*` | env vars / secrets |
| `matrix.*` | this job's matrix combination |
| `needs.<job>.outputs.*` | a direct dependency's outputs |
| `needs.<job>.result` | a direct dependency's result (success/failure/skipped/cancelled) |
| `steps.outputs.*` | earlier steps' outputs (same job) |
| `webhook.*` | webhook payload |

### Functions & operators

- Status: `success()`, `failure()`, `cancelled()`, `always()`.
- Utility: `hashFiles(glob)`, `contains(haystack, needle)`, `startsWith(s, prefix)`.
- Operators: `==`, `!=`, `&&`, `||`, `!`, comparisons, parentheses.

User-chosen names (manual inputs, matrix keys) colliding with reserved names are
flagged by validation.

---

## Matrix

`matrix` (job-level) expands the job into one pod per combination, in parallel.
`${{ matrix.<key> }}` is available in the job's steps, image, env, and cache key.
Control with the sibling job fields `failFast` (default true — cancel remaining
variants on first failure) and `maxParallel`.

```yaml
test:
  matrix: { go: ["1.25", "1.26"], os: [linux, darwin] }
  failFast: false
  maxParallel: 2
  image: golang:${{ matrix.go }}
  steps:
    - run: GOOS=${{ matrix.os }} go test ./...
```

Downstream `needs: [test]` waits for **all** variants.

---

## Local execution

```
flint run [--job NAME] [--env ENV] [--input k=v] [--secret k=v]
```

Runs the same pipeline on your machine via the **local (Podman) executor** — jobs
as local containers, scratch in a host temp dir, handoff via a host dir, **no
cluster and no object store**. Secrets come from a local source (`--secret`, env,
or a gitignored `.flint/secrets.local.yaml`). The inner loop: edit YAML → run a
job → iterate, without pushing. This is the primary authoring experience.

---

## Editor support & validation

Flint publishes a **JSON Schema** for `.flint/*.yaml`. Add a header for
autocomplete + inline validation in VS Code / JetBrains:

```yaml
# yaml-language-server: $schema=https://schema.flint.dev/pipeline.json
```

`flint validate` is the CLI equivalent (schema and validator are generated from
the same Go types). Static checks (no cluster, no fetch):

- Exactly one of `steps`/`gate` per job; ≥1 job; ≥1 trigger.
- `needs` references exist; the job graph is acyclic.
- Environment narrowing subset rules; environment-awareness consistency.
- Trigger compatibility rules.
- Expression parse + reserved-name collisions; `needs.X` referenced in `if:`/
  `outputs` is a direct need.
- `outputs` reference existing step outputs; `artifacts` globs well-formed.
- One image per job; `disk`/durations parse; secret bindings have exactly one target.

`flint simulate --environment <env>` previews which jobs run/skip and how
env/secrets resolve. Never caches — always fresh.

---

## Re-runs & debugging

- **Partial re-run.** Re-run a single failed job, or "from job X onward."
  Successful upstream jobs are **not** re-executed — their artifacts and outputs
  are restored from the object store (so a failed `deploy` re-runs without
  rebuilding). Requires run artifacts/outputs to be retained (retention is
  configurable per project).
- **Interactive debug.** Re-run a job in debug mode to **hold its pod open** after
  the steps finish (or on failure) for `kubectl exec` / a web terminal, with a
  TTL. RBAC-gated; the pod and its secrets are torn down on TTL.

---

## Worked example

[`examples/release.yaml`](examples/release.yaml) exercises matrix,
parallel/sequential jobs, cross-job outputs+artifacts, conditional jobs (inputs
and upstream outputs), a `failure()` rollback, gates, environments+promotion,
service sidecars, cache, per-job disk, concurrency, and file/env secrets. Its job
graph:

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
│ (advis)│   │ ⫶ matrix×3 │        │ + postgres svc   │        │ ⊘ if has_  │
│        │   │ ⊘ !skip    │        │ ⊘ !skip          │        │ migrations │
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
                        │      deploy       │ [CD] concurrency: serialize
                        │ kubeconfig (file) │      per environment
                        └─────────┬────────┘
                ┌─────────────────┼─────────────────┐
                ▼                 ▼                  ▼
        ┌────────────┐   ┌──────────────┐   ┌──────────────┐
        │  rollback  │   │  load-test   │   │   notify     │ [CD]
        │ ⊘ failure()│   │ ⊘ run_load_  │   │ always()     │
        │            │   │   test       │   │ use slack    │
        └────────────┘   └──────────────┘   └──────────────┘

  ⊘ conditional (if:)   ⫶ matrix → N pods   ◇ gate   [CD] has environments (skipped on PRs)
  fan-out below build = parallel; downward chains = sequential.
  skipped need = satisfied → deploy runs on staging even with approve/migrate skipped.
```

By trigger: a **PR** runs `build → lint · unit-test · integration-test` only;
**push→staging** adds `image → deploy → notify` (+`migrate` if migrations,
+`rollback` if deploy fails); **promotion→production** adds the `approve` gate.

---

## What changed from the previous spec

- Flat `steps:` + auto-coalescing ⇒ **`jobs:` (pods) → `steps:` (in-pod)**.
- Step-level `dependsOn`/`environments`/`matrix` ⇒ **job-level** (`needs`/
  `environments`/`matrix`). Steps are sequential within a pod. Gates ⇒ gate jobs.
- Cross-pod data = `outputs` + `artifacts`; within a job = shared disk.
- **`when:` removed** — unified on `if:` + status functions (`success/failure/
  cancelled/always`) + `needs.<job>.result`.
- **`env` and `secrets` at pipeline/job/step** (merged, narrower wins).
- **Secrets subsystem**: multi-source (built-in store + Vault/AWS/GCP/Azure/K8s),
  delivered as env or tmpfs files, sidecar-brokered, env-scoped, masked.
- **`concurrency`** (pipeline + job), **matrix `failFast`/`maxParallel`**, **cache
  `restoreKeys`**.
- Execution: per-job agent **sidecar** (secret broker; creds isolated; native
  sidecar, K8s ≥ 1.28), per-job ephemeral scratch, object-store handoff,
  control-plane completion, per-step log markers.
- **Module reuse system**: one `use:`/`extends:` keyword; four kinds (`action`
  containerized · `steps`/`job`/`pipeline` inline); typed inputs/outputs + a
  `steps`-hole (`inject:`); env contracts (`requires:`) with static compat
  checking; black-box composition (no deep-merge); **immutable refs, no lockfile**
  (exact version or publisher alias); registry fed by GitOps / API / local files.

### Deferred (to specify when built)

Centralized project registration (`ProjectSet` — a central repo defining many
projects + target repos); the module **registry** backing store + GitOps sync
details; image presets; matrix include/exclude.
