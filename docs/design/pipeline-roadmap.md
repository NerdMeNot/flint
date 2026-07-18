# Pipeline DSL: prioritized roadmap

Status: proposed · Companion: [`pipeline-competitive.md`](./pipeline-competitive.md)

Sequences the work to take Flint's CI YAML from "differentiated design, early-
subset implementation" to "as good as it can be." Ordered by value ÷ effort,
with the cheapest table-stakes fix first and the compounding differentiator as
its own track.

## Sequencing rationale

- **Correctness holes before features.** Failure-handling isn't a feature gap —
  it's a hole in the core conditional model, and it's the cheapest to close
  because the engine already supports it. It leads.
- **Trust before breadth.** Non-runnable examples and k8s-era docs cost adoption
  more than any missing keyword. Fix them right after failure-handling (they're
  related — the examples fail *because* of the failure-handling gap and the
  rejected features).
- **Table-stakes before the big bet.** Ship the expected-by-everyone features
  (matrix include/exclude, `action` modules) before the declared-inputs/affected
  differentiator, so the DSL is *complete* before it's *clever*.
- **The big bet is a separate track** — it's where the intelligence compounds,
  and it can proceed in parallel once the foundation is solid.

## The gaps (grounded)

Verified against the compiler, not just the spec:
- `success()`/`failure()`/`cancelled()`/`always()` fail expression compilation
  (`unknown name`); CI `Job` has no `when:`; ancestor-failed jobs skip before
  `if:` (`internal/core/engine/advance.go` gates dependencies before the
  condition). → run-on-failure/always **not expressible**.
- Matrix is cartesian-only (cap 256); `failFast`/`maxParallel` parsed but rejected
  (`validate_rich.go`); no `include`/`exclude`/adjustments.
- `action` (containerized) modules rejected at resolve (`resolve.go`).
- Rejected: `schedule`/`promotion`/`webhook` triggers, external + file secrets,
  job-level concurrency (`validate_rich.go`).
- Expression functions: a small whitelist + `hashFiles`; no `fromJSON`/`toJSON`/
  `format`/regex.
- Module registry `@version`/`@alias` is spec-level; only forge file-fetch
  (`./` and `org/repo/path@ref`) is wired (`resolver.go`).
- Docs (`pipeline-spec.md`, `execution-model.md`) describe k8s; examples
  (`release.yaml`, `payments-api.ci.yaml`) use features that don't compile.

## Phased plan

### P1 — Failure-handling / run-always (the cheapest high-impact win)

The engine already gates on dependency-scoped `when` and evaluates failure
correctly (`pkg/pipeline.Step.When`, `stepShouldRun`); the CI compiler just
doesn't expose or wire it.
- Add a job-level `when:` to the CI `Job` type (`onSuccess`|`onFailure`|`always`,
  default `onSuccess`) and thread it into the compiled `pipeline.Step` group
  (`compile.go` currently leaves `When=""`).
- Implement the status functions `success()`/`failure()`/`cancelled()`/`always()`
  in the expression whitelist (`pkg/pipeline/expr.go`), scoped to a job's
  dependency subgraph (mirror the engine's `ancestorFailed`), and make
  `needs.<job>.result` reachable for a failed need.
- Stop skipping an ancestor-failed job before its condition is evaluated when
  `when: always`/`onFailure` (the engine path exists; the compile-time gate is
  the blocker).
- *Acceptance:* the `release.yaml` rollback + always-notify jobs compile and run;
  a job with `when: always` runs after an upstream failure; `if: ${{ failure() }}`
  compiles and skips on success.

### P2 — Trust: runnable examples + de-k8s'd docs

- Make every example under `docs/design/examples/` actually compile and run
  against the current compiler (fixes land automatically once P1 + P3/P4 do; gate
  anything still unbuilt behind a clearly-labeled "planned" example).
- Rewrite `pipeline-spec.md` / `execution-model.md` execution prose for the
  machine substrate (no pods/kubectl/IRSA); keep the language surface.
- Retire or clearly mark the engine-IR flat-dialect UI preview endpoint that
  mis-parses a `jobs:` file (`routes.go`).
- *Acceptance:* a CI check compiles every `docs/design/examples/*.ci.yaml`; no
  example uses a loud-rejected feature without a "planned" label.

### P3 — Matrix depth

- `include` / `exclude` / adjustments (per-combination extra vars), matching the
  cartesian expansion in `compile.go`; keep the 256-combination cap.
- Enforce `failFast` and `maxParallel` (currently parsed-then-rejected) instead
  of rejecting them.
- *Acceptance:* `include`/`exclude` change the generated variant set; `failFast`
  cancels sibling variants on first failure.

### P4 — `action` (containerized) modules

- Wire the designed-but-rejected `action` kind (`module.go`) through resolve +
  compile so a step can run in its own image — the third-party/marketplace story.
- *Acceptance:* a `use:`-d `action` module runs its `run:{image,command}` as a
  step in the caller's job.

### P5 — Expression + trigger/secret breadth (demand-driven)

- Add `fromJSON`/`toJSON`/`format` to the expression whitelist (prerequisites for
  fan-out and dynamic config).
- Lift the remaining loud-rejections as demand warrants: `schedule:` CI cron
  (Workflows already has cron — share it), external/file secrets, job-level
  concurrency, `promotion`/`webhook` triggers.
- Back the module registry with a real store + versioned/alias resolution beyond
  forge fetch.

### The big bet (parallel track) — declared inputs, affected, content cache

The compounding differentiator. Flint already has named `outputs`, artifact
hand-off along `needs`, `cache` + `hashFiles`, and a real DAG — ~60% of the model.

- **Declared job `inputs:`** — file globs + upstream job outputs + env. The engine
  **derives the cache key from the hash of declared inputs** (no hand-written
  keys — kills GHA's #1 cache-bug class), and can **skip a job whose inputs are
  unchanged** since its last successful run.
- **Named input sets** — so a test edit doesn't bust the build cache (Nx's key
  ergonomic).
- **`affected` mode** — run only jobs whose declared inputs changed between
  `--base`/`--head`, plus their dependents (project graph ∩ git-diff). The single
  biggest CI-cost lever and directly ledger-friendly.
- **Two cache primitives** — content-addressed *output* cache vs named *volume*
  cache (the package-manager dir), instead of one leaky `cache:` block.
- **Runtime fan-out** — a step emits a JSON array, the engine spawns one child job
  per element (Argo `withParam`); a fan-in job depends on all children. Needs
  `fromJSON` (P5). Cascadable, unlike CircleCI's one-shot continuation.
- *Acceptance:* a monorepo pipeline runs only the jobs whose inputs changed; a
  cold job with unchanged inputs restores from cache without a user-authored key;
  a discover-step's JSON output fans out N test jobs.

### North stars (own design docs when picked up)

- **Resources model** — unify triggers + artifacts as versioned, typed resources
  (Concourse) for free provenance and pinning; collapses `on:`/`cache:`/
  `artifacts:`/`needs:` into one concept.
- **Reproducibility → provenance** — reward pinned/declared step environments
  (extend `requires:`) with stronger cache guarantees + auto-emitted SBOM/
  provenance, fitting the decision-ledger ethos.
- **Governance** — an `extends`-style required-template check (Azure) if
  enterprise policy enforcement becomes a need; the module system's black-box
  composition is already most of the primitive.

## Testing

- P1: compiler tests that the status functions compile and evaluate per
  dependency subgraph; a run-through test that `when: always` runs after upstream
  failure (extend the engine's existing `advance`/DAG tests, which already cover
  dependency-scoped `when`).
- P2: a CI job that compiles every example file.
- P3/P4: compiler tests for the expanded matrix set and the `action` resolve path.
- Big bet: content-hash determinism tests; an `affected` test over a synthetic
  graph + git diff; a fan-out test asserting N children from a JSON output.
- Keep the validation-correctness invariant: everything new is compile-checked
  against the exact runtime context so "validates clean = runs clean" holds.
