# Pipeline DSL: competitive analysis

Status: analysis · Companion: [`pipeline-roadmap.md`](./pipeline-roadmap.md) ·
Grounded against GitHub Actions, CircleCI, GitLab CI, Azure Pipelines, and the
"different-model" tools (Dagger, Earthly, Concourse, Argo/Tekton, Nx/Bazel,
apko/Nix), 2025–2026.

Framing constraints:
- **Staying on YAML.** CUE/KCL/Starlark were evaluated and rejected
  ([[project-pipeline-language]]); this doc does not re-litigate the config
  language. Dagger's own reversal (it abandoned CUE because users hated learning
  a new language) confirms the call: **the config language is not the moat — the
  graph / cache / execution model is.**
- **Two dialects exist.** `internal/products/ci/` is the real user-facing CI DSL
  (`jobs:` → `steps:`, entry `ci.Parse`). `pkg/pipeline/` is the engine IR +
  shared machinery (expr, triggers, matrix, DAG, validation) and also carries an
  older flat-`steps:` dialect used only by the Workflows product. Everything
  below is the **CI jobs-dialect** unless noted.
- **The prose docs are stale.** `pipeline-spec.md` / `execution-model.md`
  describe Kubernetes pods/kubectl/IRSA (pre-July-2026 pivot). Treat the language
  *surface* as real; the *execution prose* as dated. Fixing this is a roadmap
  item, not a footnote — the repo examples currently use features that don't
  compile (see below).

## Verdict

**The DSL design is more principled than GitHub Actions on reuse — in two
respects it's ahead of the entire field — but the implementation is an early
subset (~50–60% of a mature DSL), and much of the documented/exampled surface
does not run today.** "It's basically GitHub Actions" undersells the design and
flatters the implementation; both are separate facts.

## Where Flint matches or beats best-in-class

Lead with these — they are real, and two are differentiators worth marketing.

- **The module system is not a GHA clone.** It's closer to CircleCI orbs / Azure
  typed templates / GitLab components, with two touches nobody else has:
  - Four module kinds keyed on **environment stance** — `action` (containerized),
    `steps` (inline), `job`, `pipeline` (`internal/products/ci/module.go`), with
    typed inputs `string|number|boolean|enum|steps`.
  - The **`steps`-hole** (`inject: ${{ inputs.steps }}`) — pass a caller's block
    of steps into a module (`resolve.go`). Only CircleCI's `steps` param type and
    Azure's `stepList` have this; GHA and GitLab do not.
  - **`requires:` static environment contract** — a module declares
    `requires: {family, tools}` and it's checked against the consuming job's image
    *before the run* (a `yum` step on Ubuntu fails at resolve, not at minute 8;
    `resolve.go` `checkEnv`). **No other CI DSL has this.**
  - **Black-box, no-deep-merge composition** — you pass inputs, you never reach
    inside and override. This deliberately deletes the single most-cited footgun
    in GitLab (`extends` array-vs-map merge surprises) and Azure.
  - Immutable, versioned refs (`module@2.3.1` / `@stable`).
- **Validation compile-checks expressions against the *exact* runtime context**
  (`validate_rich.go` against `engine.ValidationExprContext`) → **"validates clean
  = runs clean."** GHA, GitLab, and CircleCI all fail this — they let you validate
  and then break at runtime. This is a genuine, marketable differentiator.
- **Loud-rejection policy** — schema-accepted-but-unimplemented features are hard
  errors, never silent no-ops (`validate.go`). Plus economics-aware runner pools
  (`runner:`) that no incumbent has.

## What each competitor teaches

| Tool | The lesson worth taking |
|---|---|
| **GitHub Actions** | Mostly an *avoid* list: string-only reusable-workflow inputs, no compile-time metaprogramming (users fake it with `fromJSON`), no merge/override, secret-propagation pain across reuse layers. Flint already avoids most of this. |
| **CircleCI** | The three-tier reuse ladder (`commands`→`executors`→`orbs`); typed params incl. the rare **`steps`** and **`env_var_name`** types (pass a secret's *name*, never its value); **immutable versioned packages** in a registry. Pain: `<< >>` is an opaque second language you can't pass runtime values through; dynamic config is one-shot, can't cascade. |
| **GitLab CI** | The **single unified `rules` grammar** applied at job / whole-pipeline / `include` level; `rules:variables` (set vars as a side effect of a match); `needs` with artifact control (`artifacts: true/false`, `optional`); **`!reference`** (cross-file, nestable value selection). Pain: **three overlapping reuse mechanisms with different merge rules** (its #1 footgun); an 11-level variable-precedence ladder. |
| **Azure Pipelines** | **Typed template parameters** (`type:` incl. `stepList`/`object`, `values:` enums) + **compile-time `${{ if }}`/`${{ each }}`** (generate/omit YAML structure) + **`extends` governance** (org template controls structure, fills user steps into typed holes, enforced by a resource-bound "required template" check). Pain: **three interpolation syntaxes** is a documented source of confusion. |
| **Dagger** | Content-addressed **automatic caching** (key derived from inputs, never hand-written) + two distinct cache primitives (content-addressed output cache vs named volume cache). Meta-lesson (the CUE reversal): config language isn't the moat. |
| **Earthly** | The **target + declared-dependency-edge** model: nothing crosses a target boundary unless declared → correct caching + monorepo skip-unchanged by construction. |
| **Concourse** | **Everything is a versioned `resource`** — triggers, artifacts, and outputs are the same typed, versioned I/O. Collapses `on:`/`cache:`/`artifacts:`/`needs:` into one model and gives provenance for free. Also: a deliberate *anti*-anchor stance (reuse via typed constructs, not YAML templating). |
| **Argo / Tekton** | **Runtime fan-out** (`withParam`: a step emits a JSON array, the engine spawns one child per element) — the thing static matrices can't do. Params (small) vs artifacts (large) as distinct typed I/O. |
| **Nx / Bazel / Turborepo** | **The build graph + content-hash cache + `affected` (graph ∩ git-diff)** as first-class. Skipping unchanged work (run 4 of 45 packages) beats any caching optimization. Named input sets (test edits don't bust the build cache). |
| **apko / Nix** | Declared, pinned environments → reproducible builds + SBOM/provenance as a *byproduct*. Dovetails with Flint's decision-ledger "explainable" ethos. |

## What to steal — ranked

**Table-stakes (close the gap to "mature DSL"):**
1. **Failure-handling / run-always** — the single biggest hole. `success()`,
   `failure()`, `cancelled()`, `always()` don't compile today (verified: expr
   "unknown name"), the CI `Job` type has no `when:`, and ancestor-failed jobs are
   skipped *before* their `if:` evaluates (`advance.go` gates deps before the
   condition). Rollback and always-notify — in Flint's own `release.yaml`
   example — cannot run. **The engine already implements dependency-scoped `when`
   / failure gating correctly** (`pkg/pipeline.Step.When`, engine `stepShouldRun`);
   the CI compiler just never wires it. Mostly a compile-layer fix.
2. **Matrix `include`/`exclude`/adjustments** + enforce `failFast`/`maxParallel`
   (parsed but currently rejected).
3. **`action` (containerized) modules** — designed (the kind exists) but rejected
   at resolve; this is the third-party/marketplace story.
4. **A few expression functions** — `fromJSON`/`toJSON`/`format` (needed for
   dynamic patterns and fan-out).

**Differentiators (be smarter than a GHA clone):**
5. **The big bet: declared inputs → content hash → (cache | skip-if-unchanged |
   affected), with named outputs so the engine derives the DAG.** Flint is ~60%
   there already (named `outputs`, artifact hand-off, `cache` + `hashFiles`, a real
   DAG). Missing: a job-level `inputs:` (globs + upstream outputs) that
   *auto-computes* the cache key (kills GHA's hand-written-key bug class) and
   enables **skip-if-unchanged** + an **`affected`** mode. This is the highest-
   leverage adoption and fits the economics/explainability ethos (skipping
   unchanged work is the biggest CI-cost lever and is ledger-friendly).
6. **Runtime fan-out** — a step emits JSON, the engine fans the next job over it
   (Argo `withParam`). Cascadable, unlike CircleCI's one-shot continuation.
7. **North stars:** unify triggers + artifacts as versioned **resources**
   (Concourse) for free provenance; two cache primitives (Dagger); reward pinned/
   declared environments with stronger caching + auto-provenance (apko/Nix — your
   `requires:` contract is the seed).

## What NOT to adopt

- **YAML anchors / merge-keys as the reuse mechanism.** You correctly
  bomb-guard them; modules are the right unit (Concourse agrees).
- **Multiple overlapping reuse mechanisms with different merge rules** (GitLab's
  #1 footgun). You have exactly one (modules) — keep it.
- **A third interpolation syntax.** Your compile-time-matrix-interpolation +
  runtime-`${{}}` is already a clean two-tier; don't grow into Azure's three.
- **Re-pitching a config language** — decided ([[project-pipeline-language]]).
- **String truthiness / loose equality** — you already require typed bools + a
  clean `false` to skip; don't regress to GHA's loose `==` or Azure's untyped-
  scalar truthiness.

## The through-line

The design ceiling is high — modules + the validation-correctness guarantee are
ahead of the pack. The floor is the problem: failure-handling is a hole, and the
examples don't run. Close those, then invest the intelligence where it compounds
— the declared-inputs / affected / content-cache model — and Flint's YAML is
*smarter* than the incumbents, not merely competitive. Sequencing in
[`pipeline-roadmap.md`](./pipeline-roadmap.md).
