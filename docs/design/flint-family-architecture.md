# Flint Family Architecture

> **July 2026:** the Kubernetes execution substrate described in parts of this
> document was replaced by the machine substrate — steps now run on raw
> machines via the `flint-agent` daemon, not as k8s Jobs. See
> [machine-substrate.md](machine-substrate.md) for the current execution
> architecture; the product-family layering below still holds.

## Overview

Flint is evolving from a single CI platform into a **family of related products** built on
a shared durable engine and control plane:

- **Flint CI** — a modern Kubernetes-native CI system, triggered by forge events.
- **Flint Workflows** — generic declarative workflows on Kubernetes (not git-coupled).
- **Flint Load Testing** — distributed HTTP/gRPC (and eventually database) load testing.

The products are deliberately **distinct tools** — distinct UIs, distinct binaries, distinct
CRDs, distinct mental models — in the spirit of Argo Workflows vs. Argo CD. What makes them
a *family* rather than a bundle is a shared platform (one identity, one RBAC model, one set of
runner pools), a shared design language, and first-class interop between them.

This document defines the layering, the engine-generalization work required to get there, the
repository structure, and the surfaces that bind the family together. It is a living document.

## Guiding decisions

These are settled and frame everything below:

1. **CI and Workflows share the engine. Load Testing does not (fully).** CI and Workflows are
   both "a durable DAG of steps that advance on completion" — the same execution shape. Load
   Testing is a different shape (N identical workers, coordinated ramp, high-frequency metric
   aggregation) and uses the engine only for *lifecycle orchestration*, not for load generation.
2. **Generalize the engine, then ship Workflows as the validating second surface.** Lowest risk,
   ~95% platform reuse, proves the abstraction is real before any larger bet.
3. **Load Testing keeps its data plane separate.** The engine orchestrates the lifecycle;
   load generation and metrics live in a purpose-built data plane on the shared platform.
4. **Monorepo now, structured for clean extraction later.** Distinct products do not require
   distinct repos. The repo split is a logistics decision deferred until the core stabilizes.
5. **One unified Flint app with capability toggles (revised).** Originally this was a cloud-console
   of *distinct product apps*; that was simplified to a **single application** whose top-level
   sections (CI, Workflows, Load Testing) are independent and **enabled per deployment via config**
   (`products.<name>.enabled`). One login, one deployment; the UI renders only the enabled
   capabilities and shows the rest as "Coming Soon". Admin (users, teams, SSO, RBAC, runner pools)
   is a single shared section. The backend already works this way — the engine route seam mounts a
   product's routes only when its capability is enabled. See *Unified UI* below.

## The three layers

Only one of these is product-specific. Naming the layers is the whole point — it tells you what
is shared, what varies, and where the seams must hold.

### Layer 1 — Platform (product-agnostic)

Already does not know or care that it runs CI. Reused as-is by every product:

- Orgs / users / teams / workspaces; RBAC; sessions; auth (OIDC / SAML / local); audit log; API keys
- Postgres + sqlc + goose infra (`internal/dbkit`, `internal/db`); the outbox
- The API server scaffolding; config loading
- Runner pools; the agent; workspace sync; secrets; logsink
- Kubernetes Job dispatch + informer machinery (the *mechanism*, not the CI-specific wiring)

This is the moat. No changes required for the family split.

### Layer 2 — Engine (`internal/engine/`)

A Postgres-backed durable DAG executor: workflows → waves → steps, with durable timers, signals,
retries, and child workflows. The mechanics are generic; today three CI assumptions are welded in
(see *Engine generalization* below). After decoupling, this layer is shared by CI and Workflows,
and used by Load Testing for lifecycle only.

### Layer 3 — Product semantics

Genuinely product-specific, and should stay that way:

- **CI**: pipeline YAML, forge providers, webhook handling, commit status, the `Pipeline` CRD.
- **Workflows**: the workflow definition schema, non-container step executors, the `Workflow` CRD.
- **Load Testing**: the scenario/load DSL, the load-phase executor, the metrics data plane.

## Engine generalization

This is the near-term effort. The engine has CI welded into three places; decoupling them (not
rewriting) turns it into a genuine generic core. Do these in dependency order.

### 1. Neutralize `StartWorkflowInput`

Today git is mandatory: `Repo`, `Ref`, `CommitSHA`, `TriggerType`, `WorkflowFile` are typed fields,
and the expression context (`advance.go`) hardcodes `git.sha` / `git.branch`. Push these down into
a generic `Inputs map[string]any` / context. CI populates a `git` namespace; Workflows populates
whatever it needs. The expression context reads from the map. **This is what lets a run exist
without a repo.**

### 2. Introduce `StepExecutor` + a registry

This is the linchpin — everything downstream depends on it. Today "execute a step" *is* "create a
container Pod": a hardcoded `switch step.execType` in `dispatch.go`. Replace it with a registry:

```go
type StepExecutor interface {
    Kind() string
    Dispatch(ctx context.Context, step StepSpec, env Env) (Handle, error)
}
```

The existing container/K8s logic becomes the registered `"container"` executor — **zero behavior
change for CI**, it just moves behind the interface. Then:

- Workflows registers `http`, `grpc`, `approval`, `subworkflow` executors.
- Load Testing registers a `loadphase` executor that fans out a worker set.

The engine stops caring *what* a step is. This satisfies the project rule "interfaces only where
implementations genuinely vary" — these implementations genuinely vary.

### 3. Replace `forge.ForgeProvider` in the engine with a narrow `FileGetter`

The engine only needs "fetch the definition bytes." Everything else forge-related (webhooks, commit
status, clone URLs) stays in the CI product. Touch points: `internal/engine/resolve.go`,
`internal/engine/pg_engine.go`.

```go
type FileGetter interface {
    GetFile(ctx context.Context, ref, path string) ([]byte, error)
}
```

### 4. Move triggers out of the engine

Triggers are a product concern that ultimately just call `StartWorkflow`. The CI webhook handler,
PR/push/tag matching, and commit-status reporting stay in the CI product. Workflows gets
manual / schedule / event entrypoints. The engine never learns what a "pull_request" is.

**Milestone:** after these four, `internal/engine/` is a generic durable DAG executor and CI is
*a consumer of it* rather than *the thing it is*.

### Delivery plan

Four reviewable PRs, **not** a big-bang refactor — the engine is the scariest (durability) code, so
each change must be small and independently revertable.

**Order: 1 → 3 → 2 → 4.** Step 3 (the `FileGetter` swap) is a trivial, zero-risk interface change
that sheds the engine's forge dependency, so it goes before the risky executor refactor. Step 2 is
the linchpin and carries the real design risk; step 4 is mostly a code-move and goes last.

**Step 2 must ship with a second, non-container executor** (a `noop`/`echo`, or a first cut of
`http`). Designing `StepExecutor` against only the container dispatcher would produce a CI-shaped
interface that breaks when Workflows arrives. A genuinely different second implementation proves the
seam generalizes *before* the Workflows product is built around it.

**Acceptance bar for every PR:** the existing CI test suite passes unchanged, with zero behavior
change for CI. The full Flint Workflows product is the next epic, after these four land.

## Load Testing: control plane vs. data plane

Load testing is the one product that must not be forced through the DAG executor.

- A DAG engine runs **heterogeneous steps, one pod per step, advancing wave by wave.**
- A load test runs **N identical workers simultaneously**, with a coordinated ramp / hold / stop,
  streaming **high-frequency time-series metrics** aggregated in real time (p50/p95/p99, RPS,
  error rate).

The engine orchestrates the **lifecycle** (provision generators → synchronize start → ramp → hold
→ drain → aggregate → report) via a `loadphase` executor. The **load generation and metric
aggregation are a separate data plane** — load runners plus a time-series-shaped results store,
never `step_outputs` JSONB.

Two cheap decisions now keep this door open without building it:

1. Add a `kind` discriminator on runs/workflows so a future load-test run is first-class, not a
   retrofit.
2. Keep the results path out of `step_outputs` in the mental model — CI's "outputs are small JSON
   blobs" assumption must not leak into anything reusable by load testing.

## Repository structure

Monorepo, **shared binaries** (capability-gated at runtime) and **one unified frontend app** — not
separate apps or repos. Products are separated in `internal/products/*`, not by deployment.

```
flint/
  cmd/
    server/ worker/ agent/        # shared binaries; products gated via config
    controller/ syncd/ flint/     # shared controller, IdP sync, CLI
  internal/
    core/            # SHARED: engine, db, dbkit, runner, agent, wsagent, observe,
                     #         flinterr, secretstore, crd, controller, worker
    platform/        # SHARED: auth, config, server scaffolding
    products/
      workflows/     # workflow schema + API + (http/approval) executors      [exists]
      ci/            # CI-specific code, extracted as it decouples             [planned]
      loadtest/      # scenario DSL, load-phase executor, data plane           [later]
  web/               # ONE unified app (TanStack Start)
    src/shell/       #   chrome: nav, capability switcher, auth/org context
    src/sections/    #   ci/  workflows/  loadtest/  — independent top-level sections
    src/settings/    #   shared Admin (users, teams, SSO, RBAC, runner pools)
```

### The one rule that keeps products independent

`internal/products/*` may import `internal/core` and `internal/platform`, **never each other.** The
same convention holds in the frontend: `web/src/sections/*` import `shell` and shared UI, never each
other. This is what keeps the unified app's sections genuinely independent (and a product cleanly
liftable later if it ever needs its own repo).

### When to actually split a repo

Split a product out when **both** are true: (1) the `internal/core` API has stopped churning
week-to-week, and (2) that product has earned an independent release cadence or owner. Until then
the monorepo is strictly better — cross-cutting engine changes stay atomic instead of becoming
coordinated multi-repo releases. The split becomes a `git filter-repo`, not a rewrite.

This is the deliberate divergence from the Argo model: the Argo projects are separate repos because
they share *no* engine and can release independently. Flint's binding asset *is* the shared engine,
so multi-repo only pays off once that engine is stable.

## What makes it a family

Distinct products need deliberate shared surfaces, or they're just three tools with the same logo.

- **One unified app** (see *Unified UI* below): a single Flint console — one login, one design
  system, a top-level switcher between independent sections (CI, Workflows, Load Testing). Sections
  render only when their capability is enabled; the rest show as "Coming Soon". This replaced the
  earlier "distinct product apps" idea: one app is simpler, the backend already gates capabilities,
  and the segregated sections still feel like independent tools.
- **Shared platform**: one identity, one RBAC model, one org/workspace switcher, one set of runner
  pools, one audit log across all three. A user logs into *Flint*, not into three things.
- **Shared API group**: everything under `flint.dev`; distinct CRD kinds per product (`Pipeline`,
  `Workflow`, `LoadTest`).
- **Interop as a first-class feature** — what makes it a *family* rather than a *bundle*: a CI
  pipeline triggers a Workflow; a Workflow stage kicks a Load Test; a Load Test result gates a CI
  promotion. Far easier to build and keep working in a monorepo, and the kind of integration that
  made the Argo ecosystem sticky.

## Unified UI

Flint ships as **one application** with capability-gated, segregated sections — not separate apps.

- **Capabilities are deploy-time config.** `products.ci.enabled`, `products.workflows.enabled`,
  `products.loadtest.enabled` (Load Testing currently `false` → "Coming Soon"). A
  `GET /api/v1/capabilities` endpoint reflects this and is the UI's single source of truth for what
  to render; the backend route seam already mounts a product's API only when enabled. (A future
  iteration could let an admin override these at runtime, stored in the DB — `/capabilities` is
  designed so that can layer on without UI rework.)
- **Segregated top-level sections.** `/ci/*`, `/workflows/*`, `/loadtest/*`, and `/settings/*`
  (Admin). Each section is an independent route tree + components + data hooks under
  `web/src/sections/*`; they import the shared shell, never each other. CI moves under `/ci/*` so all
  three are peers (`/` redirects to the first enabled section).
- **Shared shell + admin.** `web/src/shell` owns navigation, the capability switcher, auth/org
  context, and theming; `web/src/settings` is the one shared Admin area (users, teams, SSO, RBAC,
  runner pools) — product-agnostic and always on.
- **Coming Soon.** Disabled capabilities (today: Load Testing) appear in the switcher as a disabled
  teaser rather than hidden, so the roadmap stays visible.

This keeps the "completely independent sections" feel while being one login, one deployment, and
strictly less frontend machinery than a multi-app shell.

## Open decisions

- Naming/branding of the individual products within the family.
- Whether `kind` lives on `workflows` or `pipeline_runs` (or both) — settle when step 1 lands.

## What not to do

- Don't fork the engine three ways. It triples the surface area of the scariest (durability)
  code to avoid a decoupling that is well-scoped.
- Don't split repos during exploration — it spends optionality on release overhead.
- Don't design the full Workflows / Load Testing schemas up front. Generalize the engine, ship
  Workflows, then let real usage reveal what Load Testing's data plane needs.
- Don't touch the platform layer — it's already where it needs to be.
