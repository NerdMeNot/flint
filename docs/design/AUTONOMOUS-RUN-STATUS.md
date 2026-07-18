# Autonomous run — status

Read this first. It records what an autonomous session delivered against the plan
in `.claude/plans` (fleet provisioning hardening + pipeline DSL completeness),
the exact PRs and their merge order, and what remains with concrete next steps.

**Headline:** Track A (fleet correctness & hardening) is **complete** — 8 tested,
pushed PRs. Track C (pipeline DSL) landed **C1, C2, C3, C5**; C4 is deferred on a
runtime dependency (below). Track B (fleet economics) and Track D (pipeline "big
bet") were **not started** — their designs are preserved below as the pickup
point. Everything landed is its own draft PR: nothing is half-committed, every
PR builds + vets + passes its tests against dev Postgres.

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

## Track B — Fleet economics — NOT STARTED (design preserved)

The reliability-class work. Base: the end of Track A (`fix/fleet-small-correctness`).
Current model: `pkg/compute` has `CapacityType` = `{spot, on_demand, any}`; the
fleet does not resolve per-branch/per-event policy or group demand by run.

- **B1 — reliability-class seam + effective-policy resolution (headline).**
  `Requirements.Capacity`/`Offer.Capacity` → `{stable|interruptible|any}`;
  provider `Classes()` advertising what it supports; awsec2 maps
  on-demand↔stable, spot↔interruptible; narrow `PolicyPatch` to
  `{overflow class, objective}`; a **pure** `resolvePolicy(base, branch, event)`
  (first-match-wins — start here, it's the most testable unit); new
  `PendingProvisioningDemandByRun` (join `pipeline_runs`, group by pool/branch/
  event); `provisionPool` resolves per demand-group; ledger branch/event/
  appliedOverride. Graceful degrade: a provider without interruptible runs
  all-stable.
- **B2 — interruptible safety + mixed capacity.** The "a live multi-step run's
  holder must be `stable`" scheduling rule; capacity `{base, overflow}` with
  boots tagged floor-fill vs overflow. (Reclaim→retry is already done via #84.)
- **B3 — interruptible economics polish.** Fold `InterruptionRisk` into the
  `balanced` score; asymmetric `interruptible→stable` fallback with a `strict`
  opt-out, ledgered; extend `computetest.Fake` to price both classes + simulate
  spot-empty.

Design corpus: `docs/design/fleet/` and `competitive-buildkite.md`.

---

## Track D — Pipeline "big bet" — NOT STARTED (design preserved)

The differentiator. Flint is ~60% there (named `outputs`, artifact hand-off,
`cache` + `hashFiles`, real DAG). Base: end of Track C. **`fromJSON` (C5, #96) is
already in place as the D3 prerequisite.**

- **D1 — declared job `inputs:` → content-hash cache key + skip-if-unchanged.**
  Job-level `inputs:` (globs + upstream outputs + env); engine derives the cache
  key from the input hash; skip a job whose inputs are unchanged since its last
  success. Reuse `hashFiles` + the cache infra. **Be conservative — a wrong cache
  key is worse than no cache; gate skip-if-unchanged behind an explicit opt-in
  and lean on determinism tests.**
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
| B1–B3 economics | not started; multi-layer + DB | start with the pure `resolvePolicy` |
| D1–D3 big bet | not started; D1 needs conservative cache-key design | D3 prerequisite `fromJSON` is done (#96) |
| pipeline-spec/execution-model de-k8s prose | ~90 "pod"/k8s references need careful contextual editing | a focused doc pass (no code impact) |
| matrix `failFast: true` / `maxParallel` | engine sibling-group cancellation | same matrix-group concept as D3 |

---

## Merge order cheat-sheet

- **Fleet stack:** #84 → #85 → #86 → #87 → #88 → #89 → #90 → #91 → #92
- **Pipeline stack:** #93 → #94 → #95 → #96

The two stacks are independent (fleet off the PR-0 base; pipeline off `main`) and
touch disjoint files, so they can merge in either relative order.
