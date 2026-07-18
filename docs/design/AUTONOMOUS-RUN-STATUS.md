# Autonomous run — status

Read this first. It records what an autonomous session delivered against the plan
in `.claude/plans` (fleet provisioning hardening + pipeline DSL completeness),
the exact PRs and their merge order, and what remains with concrete next steps.

**Headline:** Track A (fleet correctness & hardening) is **complete** — 8 tested,
pushed PRs. Track C (pipeline DSL) landed **C1, C2, C3, C5**; C4 is deferred on a
runtime dependency (below). Track B (fleet economics) has its **B1 core landed**
(ResolvePolicy + reliability seam, #98); B2/B3 not started. Track D (pipeline "big
bet") has its **D1 foundation landed** (declared inputs + content-key, #99);
D2/D3 not started. Everything landed is its own draft PR: nothing is
half-committed, every PR builds + vets + passes its tests against dev Postgres.
The remaining-work designs are preserved below as the pickup point.

Every PR is a **draft**. They are **stacked** — review/merge in the order given.

---

## How to verify (per the plan)

Dev Postgres up; then per branch:

```bash
FLINT_TEST_DSN=postgres://flint:flint@localhost:5432/flint \
  go build ./... && go vet ./... && task test
```

End-to-end spot check: `task dev-sim`, log in (`admin@flint.dev` / `flintdev123`),
trigger a run. Fleet multi-replica correctness is covered by
`internal/core/fleet/*_test.go`; pipeline behavior by the runnable examples and
the ci/engine tests.

---

## Track A — Fleet correctness & hardening — COMPLETE

Stack (base first). Merge **#84 → #85 → … → #92** in order.

| PR | Branch | What |
|----|--------|------|
| #84 | `fleet/pr0-provisioning-correctness` | (pre-existing base) machine-lost retry + scheduler/scale-down race fixes |
| #85 | `fix/fleet-indexes-pool-sizing` | **A1** — indexes for the provisioning/retention hot queries; pgx `maxConns` floor (was starving the 8-way outbox fan-out) |
| #86 | `fix/fleet-optimistic-machine-transitions` | **A2** — `UpdateMachineStatus` gains a `from`-guard; the transition chokepoint treats 0 rows as a lost race (sentinel `ErrMachineTransitionRaceLost`); all 13 call sites classified benign-vs-propagate. Fixed a latent `bootMachine` clobber surfaced by the guard. |
| #87 | `fix/fleet-reconcile-live-machine` | **A3** — reconcile correlates on a stable `MachineID` tag, not the instance id, so an instance in the ref-not-yet-recorded window is never destroyed; `computetest` conformance asserts List surfaces MachineID |
| #88 | `fix/fleet-scaledown-active-guard` | **A4** — `ClaimIdleMachinesPastTTL` gains a `NOT EXISTS (live assignment)` guard; scale-down never terminates a machine holding running work; defense-in-depth signals for any work it does kill |
| #89 | `fix/fleet-retention` | **A5** — batched retention for `step_assignments` (the fattest unbounded table, orphaned by run deletion), `machine_events` (30d), `fleet_decisions` (90d), each with a supporting index, wired into the sweep |
| #90 | `perf/fleet-loop-idle-cost` | **A6** — provisioning skips the advisory-lock + per-pool count for quiet scale-to-zero pools, so the loop's floor cost no longer grows with pool count; scale-down was already gated by its single claim query |
| #91 | `fix/engine-stale-workflow-finish` | **A7** — the stale-workflow sweep routes through the real `finishWorkflow` (FinishRun + `workflow_finished` event + webhook enqueue) with a steps-derived verdict, instead of a bare UPDATE that stranded the run 'running' and always wrote 'failed'; fixes a latent no-steps window |
| #92 | `fix/fleet-small-correctness` | **A8** — five fixes: idempotent `CompleteAssignment` (`FinishAssignment :execrows`); `GetCloneToken` liveness gate; drop expired offers before booting; `requirementsFromPool` populates `req.GPU`; operator drain is ledgered. Plus an `elasticHarness` de-flake (t.Logf from a detached goroutine panicked the suite). |

All A-track findings came from the 5-agent architecture review; the two
highest-severity items were already fixed on #84. **A is done.**

---

## Track C — Pipeline DSL completeness — C1, C2, C3, C5 done; C4 deferred

Stack off `main` (separate from the fleet stack). Merge **#93 → #94 → #95 → #96**.

| PR | Branch | What |
|----|--------|------|
| #93 | `feat/pipeline-failure-handling` | **C1 (P1)** — job-level `when:` (onSuccess/onFailure/always) threaded through compile into `pipeline.Step.When` (the engine already gated on it); `success()`/`failure()`/`always()` status functions in `if:`, scoped to the dependency subgraph, injected in the single builder shared by runtime + validation (no drift). Enables rollback-on-failure and always-notify jobs. |
| #94 | `docs/pipeline-examples-runnable` | **C2 (P2)** — `docs/design/examples/runnable/` with a compile-enforcement test (every example must Parse+Compile clean for each environment). README separates runnable from the aspirational registry-based sketches. |
| #95 | `feat/pipeline-matrix-depth` | **C3 (P3)** — matrix `include`/`exclude` (GitHub-Actions semantics) via a new `MatrixSpec` type; validates exclude keys + the final combination count; interpolates include-only keys; accepts `failFast: false` (the current behavior). |
| #96 | `feat/pipeline-expr-breadth` | **C5 (P5, expr core)** — `fromJSON`/`toJSON`/`format` in every expression context via one `StdExprFuncs()`. `fromJSON` is the D3 prerequisite. |

### C4 (P4, action modules) — DEFERRED, with reason

`action` (containerized, own-image) step modules resolve to a step that must run
in its **own container mid-job**. The agent's steps driver runs all sub-steps in
**one** job container (it is that container's main process; `agentd/executor.go`
sets the image per-job). Per-step container execution is a **daemon runtime
feature**, not a compile-layer change. `resolve.go` already rejects action
modules with a clear, honest message, so there is **no silent-wrong risk today**.
Landing only the resolve/compile half would either sit unused or risk a step
running in the wrong image — violating the "validates-clean-runs-clean"
invariant. Deferred until the daemon gains container-per-step launch (shared
workspace volume, separate image). This matches the plan's stated caveat for C4.

### C5 remainder (trigger/secret breadth) — DEFERRED

The expression functions (the high-value, D3-enabling core) landed. The
trigger/secret breadth — CI `schedule:` cron (share the Workflows scheduler),
job-level concurrency, external/file secrets — is separate wiring, currently
loud-rejected in `validate_rich.go`/`checkTriggers`. Next step: lift each
loud-rejection with its enforcement path.

---

## Track B — Fleet economics — B1 core landed (#98); B2/B3 not started

The reliability-class work. Stack: `feat/fleet-reliability-policy` (#98) off
`fix/fleet-small-correctness` (#92).

- **B1 — reliability-class seam + effective-policy resolution — LANDED (#98).**
  Two units in the PR:
  - **`ResolvePolicy(base, branch, event)`** (`runner/policy.go`): pure,
    first-match-wins application of the pool's per-branch/event overrides — which
    were already loaded from the DB but never applied (dead data). Exhaustively
    table-tested.
  - **Reliability seam**: `CapacityType` reframed as a reliability contract
    (on_demand = stable, spot = interruptible); provider `Classes()` advertises
    what it supplies; `DegradeCapacity` resolves a request to a supported class
    with an asymmetric rule (interruptible→stable upgrade when needed, never the
    unsafe reverse); `provisionPool` degrades before quoting so a spot pool on a
    stable-only provider (localdev) boots instead of silently starving.
    Conformance asserts a provider advertises the classes it quotes.
  - **Remaining B1** (next unit): `PendingProvisioningDemandByRun` (join
    `pipeline_runs`, group by pool/branch/event) → `provisionPool` calls
    ResolvePolicy per demand group and ledgers the applied override. The two
    landed pieces are its inputs.
- **B2 — interruptible safety + mixed capacity.** The "a live multi-step run's
  holder must be `stable`" scheduling rule; capacity `{base, overflow}` with
  boots tagged floor-fill vs overflow. (Reclaim→retry is already done via #84.)
- **B3 — interruptible economics polish.** Fold `InterruptionRisk` into the
  `balanced` score; asymmetric `interruptible→stable` fallback with a `strict`
  opt-out, ledgered; extend `computetest.Fake` to price both classes + simulate
  spot-empty.

Design corpus: `docs/design/fleet/` and `competitive-buildkite.md`.

---

## Track D — Pipeline "big bet" — D1 foundation landed (#99); D2/D3 not started

The differentiator. Flint is ~60% there (named `outputs`, artifact hand-off,
`cache` + `hashFiles`, real DAG). Stack: `feat/pipeline-content-cache` (#99) off
`feat/pipeline-expr-breadth` (#96). **`fromJSON` (C5, #96) is already in place as
the D3 prerequisite.**

- **D1 — declared job `inputs:` → content-hash cache key + skip — FOUNDATION
  LANDED (#99).** The conservative core (the plan flags this as the be-careful
  item): `Job.Inputs` (files globs + upstream `needs` outputs + env), validated
  (inputs.needs tied to needs:); and the pure, exhaustively-tested
  `DeriveContentKey` (sorted, length-prefixed, namespaced, injection-proof
  sha256 — deterministic and collision-safe). **Deliberately NOT yet wired**
  (next units, both consume DeriveContentKey): synthesize the derived key into
  the compiled step's cache, and the **opt-in** skip-if-unchanged engine
  decision. A wrong cache key is worse than no cache — hence key-primitive-first,
  skip-behind-opt-in.
- **D2 — `affected` mode.** Run only jobs whose declared inputs changed between
  `--base`/`--head` + dependents (project graph ∩ git-diff).
- **D3 — runtime fan-out.** A step emits a JSON array → engine spawns one child
  job per element + a fan-in job. Needs `fromJSON` (done) + a matrix-group
  concept in the engine (also what matrix `failFast: true` needs).

---

## Deferred items summary (so nothing is silently dropped)

| Item | Why deferred | Unblocks it |
|------|--------------|-------------|
| C4 action modules | needs per-step container execution in the daemon | `agentd` container-per-step launch |
| C5 trigger/secret breadth | separate wiring; expr core landed | lift each loud-rejection with its path |
| B1 remainder | per-run demand grouping + provisioner wiring | `PendingProvisioningDemandByRun` + call ResolvePolicy per group |
| B2 interruptible safety, B3 economics polish | not started; need base/overflow capacity design | after B1 remainder |
| D1 skip-if-unchanged | risky; foundation (key primitive) landed | wire DeriveContentKey → cache; opt-in skip |
| D2 affected mode | not started; needs git-diff ∩ project graph | declared inputs (#99) provide the graph |
| D3 runtime fan-out | not started; needs engine child-job spawning | `fromJSON` done (#96); same matrix-group concept as failFast |
| pipeline-spec/execution-model de-k8s prose | ~90 "pod"/k8s references need careful contextual editing | a focused doc pass (no code impact) |
| matrix `failFast: true` / `maxParallel` | engine sibling-group cancellation | same matrix-group concept as D3 |

---

## Merge order cheat-sheet

- **Fleet stack:** #84 → #85 → #86 → #87 → #88 → #89 → #90 → #91 → #92 → **#98** (B1)
- **Pipeline stack:** #93 → #94 → #95 → #96 → **#99** (D1)

The two stacks are independent (fleet off the PR-0 base; pipeline off `main`) and
touch disjoint files, so they can merge in either relative order. (#97, this doc,
is off `main` and stands alone.)
