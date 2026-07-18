# Fleet: roadmap

Status: proposed · Part of the [fleet design set](./README.md)

Sequences the work from the [model](./economics.md) and the
[hard cases](./hard-cases.md). **Reprioritized** from the first draft: two
soundness bugs and the interruptible-durability rule now come *before* the
spot-economics polish, because shipping cheap-but-unsafe capacity or a
scale-out that over-provisions is worse than shipping the headline feature a
week later.

## The gaps (motivation)

What's scaffolded-but-unwired or missing, by economic leverage:

1. **Per-branch/event overrides: declared, parsed, exposed — never applied.**
   The column, the `PoolSpec.Policy.Overrides` parse, and the API exist, but
   provisioning reads policy straight off the raw pool row
   (`provisioner.go:70,104,279`) with no branch/event in scope. *90% built.*
2. **`InterruptionRisk` unused; no spot/interruptible fallback**
   (`rankOffers`, `provisioner.go:405`).
3. **No utilization/predictive scaling** — deficit + min-warm only.
4. **No snapshot/warm-boot** — "warm" = N idle machines.
5. **Correctness bugs:** `req.GPU` dropped (`provisioner.go:371`); single-provider
   ranking only; `drain` decision never written.

Plus the soundness/tension findings from [hard-cases](./hard-cases.md), which
reorder everything below.

## Sequencing rationale

- **Correctness before polish.** A1/A2 (provisioning race, affinity over-count)
  are wrong *today* in scale-out. They land first — a beautiful spot ranker on
  top of a double-provisioning loop is negative value.
- **Safety before defaults.** B1 (interruptible reclaim loses a run's local
  workspace) must be resolved before interruptible is the recommended overflow
  default. Otherwise the headline "cheap burst" quietly corrupts long runs.
- **Model before mechanism.** The reliability-class seam (PR 1) is the vocabulary
  everything else speaks; it lands early even though it's "just" a rename,
  because every later PR reads it.

## Phased plan

### PR 0 — Provisioning correctness (soundness; gates scale-out)

- **A1:** wrap `provisionPool` per pool in `pg_advisory_xact_lock(pool_id)` (or a
  claim-based `provisioning_intent`), so N dispatch replicas can't N×-provision.
- **A2:** split provisioning demand into "placeable on new capacity" vs
  "affinity-pinned to a live holder"; the deficit only counts the former.
- **A3 (audit):** verify committed-capacity release on every terminal edge;
  reconcile committed-vs-actual on `→ lost`.
- *Acceptance:* two `--mode dispatch` replicas against one pool never exceed
  `max_machines`/`min_warm`; a run blocked on a full holder does not trigger a
  boot.

### PR 1 — Reliability-class seam + effective-policy resolution (the headline)

- Move the seam to classes: `Requirements.Capacity`/`Offer.Capacity` →
  `{stable|interruptible|any}`; add `Classes()`; awsec2 maps on-demand↔stable,
  spot↔interruptible; localdev/staticpool stable-only. Pre-live, three providers.
- Narrow `PolicyPatch` to `{Overflow (class), Objective}` (`runner/types.go:82`);
  standing capacity stays pool-level.
- Add `resolvePolicy` (`internal/core/fleet/policy.go`) — pure, first-match-wins,
  table-tested.
- New query `PendingAssignmentDemandByRun` (join `pipeline_runs`, group by
  `(pool_id, branch, event)`); `provisionPool` resolves per group and threads
  `(overflow class, objective)` into `requirementsFromPool` + `rankOffers`.
- Ledger `inputs` gains `branch`, `event`, `appliedOverride`.
- *Acceptance:* a `{base: stable, overflow: interruptible, min_warm: 3}` pool with
  a `pull_request → interruptible` override keeps exactly 3 stable warm, ledgers
  the class + override per boot, and the **same spec on localdev runs all-stable
  with no error**.

### PR 2 — Interruptible safety (must precede making it a default)

- **B1 scheduling rule:** the holder of an in-progress multi-step run must be
  `stable`; `interruptible` is only chosen for fresh/short work. Encode as a
  scheduler constraint, not a hope.
- **Duration signal:** estimate job duration from history; long jobs prefer
  `stable` even in an interruptible bucket.
- **Safe-by-default reclaim:** a step killed by fleet drain or reclaim
  (`heartbeat.go:68`) requeues via the existing signal path
  (`failStepsViaSignals`, `fleet.go:125`) and is ledgered — no user YAML.
- *Acceptance:* reclaiming an interruptible machine mid-run never silently loses
  a run; a long job is not placed on interruptible capacity.

### PR 3 — Interruptible economics polish

- **Directed mixed capacity:** tag each boot as floor-fill (`warmShort`) vs
  overflow, select `capacity.base` vs `capacity.overflow`; extend pool capacity
  config scalar→`{base, overflow}`. Ledger the bucket per boot.
- Fold `InterruptionRisk` into `balanced`:
  `price × (1 + boot/300) × (1 + risk × k)`; keep `cost`/`latency` pure.
- **Asymmetric fallback:** `interruptible`/`any` empty → retry `stable`, ledger
  `fallback: interruptible→stable` with the price delta; never the reverse;
  `strict: true` opts out.

### PR 4 — Provisioning efficiency (D3)

- Quote once, Create N (batched/parallel, concurrency-capped) instead of
  Quote-per-machine + serial Create.
- Scale-in cooldown to stop flapping.
- Enforce `Offer.ExpiresAt` at Create time (A4); re-Quote if stale.

### PR 5 — Ledger completeness + correctness fixes

- Write the `drain` decision (currently event-only).
- Fix `requirementsFromPool` to populate `req.GPU`.
- Multi-provider pools: optional provider list; `rankOffers` chooses across
  providers (keep single-provider default). Ranking must stay explainable from
  one ledger row.
- Replace reconcile's in-memory `zombieSightings` (`reconcile.go:83`) with a
  provider-supplied create timestamp; Destroy leaked instances on boot-timeout
  (C2).
- **A5:** `MachineRef` carries `MachineID` (from the instance tag); reconcile
  correlates on it, not the instance-ID. Add "tag with MachineID + surface it in
  `List`" to `computetest.RunConformance` so idempotent reconcile is guaranteed,
  not accidental.
- **Cost attribution (D1):** core-seconds split for shared machines; standing
  capacity as an explicit pool line item.

### Verify-now (possible live bugs, not features)

Ahead of any new feature work, confirm/fix — these are cheap if right, security
holes if wrong (see [security.md](./security.md)):
- **S2** task token is step-lifetime-scoped, step-bound, revoked on completion.
- **S3** each step gets its own pid + mount (+ user) namespace, not just network.
- **S4** workspace cleanup is eager on run-terminal / before machine reuse, and
  secret *files* live on per-step tmpfs, never the shared workspace tree.

### Near-term guardrail (before real money is at stake)

- **F1/F2** org-level (and workspace) machine + **spend cap** that gates
  provisioning, ledgered as `budget_exceeded`; verify matrix-size bounds. A cost
  platform with no budget stop is a liability, so this rides early.

### Later tracks (own docs)

- **Multi-tenant isolation (B2/S1/S5):** `isolation: shared|dedicated` +
  pool-usage RBAC + data-residency hard constraint (S7) — a security *and* cost
  boundary.
- **GPU sub-fleet (B3):** GPU-aware scheduling, long boot deadlines,
  stable-by-default, GPU-hour economics.
- **Provider health/failover (C1)** + reconcile-at-scale (C4).
- **Abstraction leaks (E2):** placement/spread, disk-type/local-NVMe, custom
  image — universal intent, provider-owned translation, per the class template.
- **Utilization/predictive scaling** and **snapshot warm-boot** (the Canva
  cache-cost lever; `rankOffers` already prices boot latency, so a warm-snapshot
  offer wins naturally once providers emit it).
- **Priority/fair-share provisioning (D2)** and **cache-aware scale-down (D4)**.
- **Control-loop tuning (G1):** boots-per-tick velocity cap, windup check,
  predictive term; **provision-time shape selection (G2)**; **pool-reconfig
  semantics (G3)**.
- **Operability:** per-run "why am I queued" (H1), infra-vs-user retry budget
  split (H2), estimated-vs-actual cost reconciliation (H3).
- **Cloud-account quota awareness (F3)** and **token/registration hardening (S6)**.

## Testing strategy

- `resolvePolicy`, `rankOffers` (incl. `InterruptionRisk`): pure, table-driven.
- Provisioning correctness (A1/A2): concurrent-worker test asserting no
  over-provision; affinity-blocked-demand test asserting no spurious boot.
- Interruptible safety (B1): simulate reclaim mid-run via `computetest`'s `Fake`;
  assert requeue + no workspace loss + long-job placement on stable.
- Fallback (PR 3): `Fake` prices stable + interruptible, simulate
  interruptible-empty, assert the ledgered `interruptible→stable` decision.
- End-to-end: `task dev-sim` + localdev extended to price an interruptible variant
  so the whole path is watchable in the ledger + insights UI.
- Every new provider: `computetest.RunConformance`.

## Prebuilt-fleet roadmap

ec2 (done) → hetzner (cost-obsessed early adopters, unserved by Buildkite) →
k8s-as-provider (beats agent-stack-k8s by requesting *nodes* through this same
engine) → gce/azure → fargate → static/localdev (done) → macos later. Each is "a
`compute.Provider` + a curated image + a sane default pool policy."
