# Fleet: hard cases & tensions

Status: analysis · Part of the [fleet design set](./README.md)

Adversarial stress-test of the [model](./economics.md). Each item is a place the
design breaks, two mechanisms collide, or a cloud-ism leaks. Grounded in the
current code. The [roadmap](./roadmap.md) sequences the fixes — several of these
are soundness bugs that gate scale-out and reshape the plan, not "nice to haves."

Severity legend: **[soundness]** correctness bug · **[tension]** design
collision needing a decision · **[robustness]** failure-mode gap ·
**[economics]** explainability/efficiency hole · **[leak]** cloud-ism in the core.

---

## A. Soundness — correctness, gates scale-out

### A1. Provisioning is racy across dispatch workers **[soundness]**

`--mode dispatch` is the scale-out story and is meant to run as multiple
replicas. The **engine** loop is multi-replica-safe (`FOR UPDATE` + `SKIP
LOCKED`). The **fleet** loop is bundled into the same `StartWorker`
(`worker.go:101`) and `Provision()` has **no cross-worker guard**
(`provisioner.go:26-53`): it reads `CountPoolMachinesByStatus`, computes
`needed`, and boots. Two replicas both see the same deficit and both fill it →
up to **N× over-provisioning**, blowing past `max_machines` and `min_warm`.

*Fix:* wrap `provisionPool` per pool in `pg_advisory_xact_lock(pool_id)`, or make
provisioning claim-based (a `provisioning_intent` row per unit of deficit,
claimed `SKIP LOCKED`). Until then, advertise "one provisioning replica."

### A2. Over-provisioning for affinity-blocked demand **[soundness]**

`PendingAssignmentDemand` counts *all* `status='pending'` per pool
(`step_assignments.sql:112`) — including assignments pending precisely because
their run's holder machine is full (warmth affinity, `scheduler.go:145` "never
split a run's workspace"). A new machine **cannot** serve those (they can only
run on the holder), so the provisioner boots machines that sit idle while the
work still waits.

*Fix:* the provisioning deficit must exclude assignments whose run has a live
holder. Split demand into "placeable on new capacity" vs "affinity-pinned to an
existing machine."

### A3. Capacity-accounting drift **[soundness]**

`busy → idle` fires on the last active assignment completing (`claim.go:53`), and
machines carry `committed_cpu_millis`. If an agent dies after claiming but before
reporting, does committed capacity leak until the heartbeat-lease `→ lost` sweep
runs? A drifting committed count makes the scheduler's free-capacity math wrong
(over- or under-packing).

*Fix:* reconcile committed-vs-actual on the lost transition; audit that every
claim path has a matching release on every terminal edge.

### A4. Quote staleness at Create time **[soundness]**

`Offer.ExpiresAt` exists (spot prices drift) but nothing enforces it. If `Create`
runs after expiry — plausible when Creates queue behind slow ones (see B-side
efficiency) — you launch at a price you never ranked, or an instance type that's
since gone unavailable.

*Fix:* gate `bootMachine` on `ExpiresAt`; re-Quote the group if the chosen offer
is stale.

### A5. Reconcile correlates by instance-ID, not the durable MachineID **[soundness]**

`Create` is idempotent per `MachineID` (awsec2 uses it as the EC2 `ClientToken`)
and *does* tag instances `flint:machine-id=<MachineID>` (`awsec2.go:222,228`) —
good. But `List` returns each `MachineRef` keyed by the **AWS instance ID**, not
that tag (`awsec2.go` List), and reconcile matches on instance-ID. So a crash
*after* Create (instance launched + tagged) but *before* the second tx records
the instance ref leaves a legitimately-owned instance that reconcile can't
correlate to its machine row → flagged a **zombie and Destroyed**, then
relaunched. The idempotency the `ClientToken` bought is defeated by the
correlation key.

*Fix:* `MachineRef` carries `MachineID` (from the tag), and reconcile correlates
on it. Make "tag every instance with its MachineID, and surface it in `List`" a
**`computetest` conformance requirement** so every provider gets idempotent,
crash-safe reconciliation — not just awsec2 by luck.

---

## B. Deep tensions — collisions needing a decision

### B1. Interruptible capacity vs run-level workspace affinity **[tension]** ← the big one

Warmth affinity is a **hard guarantee keyed on `RunID`**: `pickMachine` pins
every assignment of a run to the machine holding its workspace, and if that
machine is full it makes the work *wait rather than move* (`scheduler.go:136,145`;
workspace is a local dir, `scheduler.go:126`). Now make that holder
`interruptible`: when the provider reclaims it, **the whole run's local workspace
vanishes** — not one step, all accumulated on-disk state. The safe-by-default
requeue re-runs the killed step, but on a machine with no workspace; how much
survives depends entirely on what's been pushed to S3, which is unspecified.

Consequences:
- **`base: stable` is a durability requirement, not just a warmth preference** —
  long-lived runs must not sit on reclaimable machines.
- Implies a scheduling rule the model lacks: *the holder of an in-progress
  multi-step run should be `stable`; `interruptible` is for fresh/short work.*
- Which implies a **duration signal** (short job → interruptible fine; 40-min job
  → reclaim near the end is pure waste). Estimate from history, like test-timing.

*Decision needed before interruptible becomes a default:* (a) never place a
live multi-step run's holder on interruptible; (b) S3-checkpoint the workspace so
reclaim is recoverable; (c) accept whole-run restart on reclaim. Recommend (a)
now, (b) as a track.

### B2. Bin-packing vs isolation — the multi-tenant boundary **[tension]**

Machines bin-pack steps from **multiple runs / workspaces** (scheduler best-fit).
Per-step container + CNI-network-namespace isolation exists (agentd), but it's a
shared host/kernel — a weaker boundary than separate machines. Two questions the
model doesn't answer:

- **May a machine be multi-tenant at all?** Needs an `isolation: shared |
  dedicated` pool knob. `dedicated` = no cross-tenant packing = more machines =
  more cost. This is simultaneously a security *and* an economics decision.
- **Who may target a pool?** A pool carries a provider + creds + network reach; a
  pool wired into a prod VPC is a **privilege boundary**. If any pipeline can say
  `runner: prod-pool`, that's arbitrary-code-execution with prod network/creds.
  Needs pool↔environment/workspace scoping through the existing Casbin RBAC.

*Fix:* an isolation knob + pool-usage RBAC. Economics and trust are entangled
here; the ledger should record isolation mode so cost differences are explainable.

### B3. GPU is almost its own sub-fleet **[tension]**

The dropped-`req.GPU` bug (`provisioner.go:371`) is the tip. GPUs break most
assumptions:
- **Scheduling**: GPUs are exclusive (or fractional via MIG/time-slice —
  hardware/provider-specific), not CPU-bin-packable. The scheduler's fit function
  (`scheduler.go`, best-fit by CPU) ignores GPU entirely.
- **Scarcity**: spot GPUs are reclaimed constantly; on-demand GPUs are
  capacity-constrained. `interruptible` is especially brutal for long GPU jobs
  (ties to B1) → GPU pools want `stable` strongly by default.
- **Boot**: big images + drivers → 10 min+ boots break `bootDeadlineFor` (see C3).
- **Cost**: GPU-hours dominate the bill → GPU pools need their own economics view.

*Fix:* treat GPU as a distinct track — GPU-aware scheduling (exclusive/fractional),
long boot deadlines, stable-by-default, GPU-hour cost attribution.

### B4. The reclaim/drain window: finish vs kill **[tension]**

Distinct from B1 (workspace loss): when a machine is drained (idle-TTL is
fine — it's idle; but spot reclaim gives ~30 s–2 min notice while a step is
*running*), does the agent (a) let the step finish if it fits the window, (b)
kill it immediately and requeue, or (c) kill at a grace deadline? Buildkite's
lifecycled waits up to an hour for graceful drain. The choice interacts with the
step's own `timeout`, and getting it wrong either wastes the notice window or
kills work that would've finished in 20 s.

*Fix:* an explicit drain-grace policy (finish-if-under-remaining-notice, else
requeue) bounded by the reclaim deadline; ledger the outcome.

---

## C. Robustness — failure modes

### C1. Provider outage / credential rotation **[robustness]**

A single-provider pool during a provider outage or with expired credentials: all
`Quote`/`Create` fail. There's no failover (multi-provider is future), and the
`no_capacity` debounce (5 min, `provisioner.go:357`) **masks a persistent outage
as if it were transient scarcity**. Running agents are unaffected (they
authenticate with their own tokens, not provider creds) — only new capacity
stalls, silently.

*Fix:* per-provider health/circuit-breaker; classify errors (credential vs
capacity vs transient) distinctly in the ledger; surface "provider unhealthy" as
a first-class state; failover to a secondary provider where configured.

### C2. Partial Create / leaked instances **[robustness]**

`Create` launches an instance but the agent never registers (bad image, network,
token) → boot deadline → `failed`. The instance is **leaked and billing** until
reconcile Destroys it — and reconcile only acts on the *second* sighting
(`reconcile.go:207`), widening the window. A `Destroy` that errors leaves the
machine stuck `terminating`.

*Fix:* Destroy immediately on boot-timeout rather than waiting for reconcile; use
the provider-supplied create timestamp (already planned) instead of sighting
counts; add a leaked-instance metric.

### C3. Boot deadline vs slow images **[robustness]**

`bootDeadlineFor` = 3× expected boot, floor 5 min (`provisioner.go:419`).
GPU/large custom images pull for 10 min+. Under-estimate the expected boot and
the deadline expires mid-pull → false `boot_timeout` → churn (terminate,
re-provision, repeat).

*Fix:* per-pool/per-image boot expectation; learn from historical boot times
(the ledger already records `boot_ok`+seconds).

### C4. Reconcile at scale **[robustness]**

`Reconcile` calls `provider.List` every ~30 ticks per provider (`loop.go:76`). At
10k machines that's a huge, paginated, rate-limited call (AWS `DescribeInstances`
throttling), diffed every cycle.

*Fix:* incremental/cursor-based reconcile, provider-side pagination, backoff on
throttle; reconcile a shard per cycle rather than the whole fleet.

### C5. Control-plane downtime blast radius **[robustness]**

Agents run their current work autonomously if the control plane is down (good),
but **all economics stops**: no provisioning (a traffic spike just queues), no
scale-down (idle machines burn money), no reconcile (zombies accumulate). The
fleet loop's availability is itself an economic parameter. A control-plane
outage during a spike degrades latency; during idle it degrades cost; either way
the ledger goes blind.

*Fix:* the fleet loop needs the same HA story as the engine dispatch loop
(A1's advisory-lock/leader work is a prerequisite), plus a "catch-up" reconcile
sweep on restart to reap what accumulated while it was down.

### C6. Loop + Quote load at scale **[robustness]**

The 1s tick runs `PendingAssignmentDemand` + `CountPoolMachinesByStatus` per pool
every second; with many pools/machines that's real DB load at a fixed cadence.
And `Quote` (D3's per-machine spot-price call) has provider API rate-limit (and
sometimes $) cost. The control loop's own cost scales with fleet size.

*Fix:* adaptive tick cadence (back off when idle), coalesce per-pool queries, and
the Quote-once batching from D3.

---

## D. Economics completeness — explainability & efficiency holes

### D1. Cost attribution under bin-packing + idle **[economics]**

The pitch is a legible cost ledger, but a bin-packed machine runs steps from
**multiple runs**, so per-run cost is a *split* of the shared machine's cost (by
core-seconds? wall-clock?) — and that model isn't defined. Worse, **idle
`min_warm` capacity costs money attributed to no run at all** (pool overhead). If
the run-cost view can't say "your share of a shared box" and the pool view can't
say "$X/day to keep 3 warm," explainable economics has a hole.

*Fix:* define a cost-attribution model (core-seconds split for shared machines;
standing-capacity cost as an explicit pool line item). The run cost chip + a
pool-economics view both consume it.

### D2. Provisioning is priority-blind and unfair **[economics]**

The engine has step **priority**, but `Provision()` works off an aggregate
pending *count* per pool with no notion of *whose* work or what priority. Under
`max_machines` contention, which run gets the one scarce new machine is implicit
(scheduler claim order). Two workspaces sharing a pool: one's burst starves the
other.

*Fix:* priority- and fair-share-aware provisioning; optional per-workspace
quotas within a shared pool.

### D3. Provision-loop efficiency: thundering herd + flapping **[economics]**

`provisionPool` does one `Quote` **per machine** in `for range needed`, then a
sequential `bootMachine` each (`provisioner.go:95-109`). A 200-assignment
monorepo push = 200 Quotes + 200 serial Creates at seconds apiece → minutes of
latency and needless quote spam. And there's **no scale-in cooldown**, so
burst → finish → idle-TTL → scale-down → next burst re-provisions: flapping.

*Fix:* Quote once, Create N (batched/parallel with a concurrency cap); add a
scale-in cooldown (Buildkite added exactly this).

### D4. Cache-aware scale-down **[economics]**

Scale-down holds `min_warm` by *count* (`provisioner.go:279`), so it may
terminate the machine with the valuable warm git-mirror / dependency cache and
keep a cold one.

*Fix:* value-aware retention — prefer keeping machines with the most useful
caches. Ties to the snapshot/warm-boot track.

### D5. Region placement economics **[economics]**

A no-holder run's first machine is provisioned by offer ranking, which can land
it in an expensive or far region; warmth affinity then pins the *whole run*
there (`scheduler.go`), and every cache/artifact pull crosses regions from S3 —
data-transfer cost the ranker never counted. Region is currently a free-form
`Requirements.Regions` preference with no cost model.

*Fix:* price cross-region transfer into ranking (or a region-affinity objective);
prefer the region holding the run's S3 workspace/cache.

---

## E. Semantics & clarity

### E1. `any` is not a directed mix **[leak-adjacent]**

`capacity: any` + `objective: cost` → always picks the cheapest = effectively
all-interruptible. "any" means *"I don't care about reliability,"* not *"give me
a balanced mix."* The directed mix is `base`/`overflow`, not `any`. Document this
so no one expects `any` to balance.

### E1b. Two more clarity traps **[clarity]**

- **`max_machines` includes `min_warm`.** `headroom = max_machines − live` and
  `live` counts warm machines, so `{min_warm: 3, max_machines: 5}` allows only
  *2* burst machines, not 5. Defensible as a ceiling, but people read "3 always +
  5 burst = 8." State it: `max_machines` is a total cap.
- **The fleet's scope is container steps only.** `http`/in-process steps run in
  the engine and create no `step_assignments`, so they're invisible to
  provisioning — correct, but worth saying so nobody expects the fleet to scale
  for an http-only pipeline.

### E2. Abstraction leaks beyond spot **[leak]**

`spot` was the first cloud-ism; `Requirements` today also can't express several
universal *intents*, forcing them into freeform provider config or raw strings:

- **Placement / spread / anti-affinity** — "spread my 3 warm machines across
  zones so one AZ loss doesn't take all three" is universal intent; mechanism is
  provider-specific (AWS AZ, Hetzner location, k8s topology). Not modeled.
- **Disk *type* / local NVMe** — `DiskGB` is universal, disk *type* isn't, and
  fast local scratch is the single biggest CI-cache lever (the Canva story).
  Economically huge, currently invisible.
- **Custom image / AMI** and **network placement** (also a security boundary,
  B2). Provider-specific; each needs a deliberate home.

*Fix:* apply the reliability-class treatment generally — universal intent in the
core, provider-owned translation, graceful degrade where absent. `runner/sizes.go`
(abstract CPU/mem → provider catalog) is the template.

### E3. Deferred-but-noted axes **[tension]**

- **Reserved / committed capacity** (Savings Plans, CUDs, reservations): a third
  axis — prepaid, so stable-and-cheaper but you pay regardless. A billing
  construct; a reserved instance looks `stable` at boot. The fleet benefits
  automatically without modeling it, *unless* we want a `prefer-reserved`
  objective. Out of scope; noted so two classes aren't mistaken for the whole
  pricing story.
- **Interruption notice period** varies (AWS 2 min, GCP/Azure ~30 s). The
  drain/requeue path handles whatever arrives; whether `Offer` should carry it so
  ranking prefers longer notice is deferred.
- **Per-branch `idle_ttl`**: only playable if attributed per-machine at boot (the
  TTL a machine was born with), not per-run. Revisit on real demand.

---

## F. Guardrails & limits — keeping the fleet from hurting you

### F1. No org/account-level cap — only per-pool `max_machines` **[economics]**

The only ceiling is per-pool `MaxMachines` (`provisioner.go:78`,
`runner/types.go:62`). An org with 20 pools at `max: 50` can stand up 1,000
machines with **no aggregate cap**. There is no org-wide machine ceiling and no
**spend/budget** guardrail — nothing that says "stop provisioning past $X/day."
For a platform whose pitch is cost control, the absence of a hard budget stop is
a glaring omission.

*Fix:* an org-level (and optional workspace-level) machine + spend cap that gates
provisioning, ledgered as a distinct `budget_exceeded` decision so "why did my
build queue" has an honest answer.

### F2. Runaway provisioning from pipeline misconfig **[economics]**

A matrix explosion (`1000 × 1000`) or a pathological fan-out compiles to tens of
thousands of `step_assignments`; the provisioner will try to satisfy that demand
up to each pool's `max`. With generous pool limits and no org cap (F1), a single
bad commit can spend real money in minutes.

*Fix:* per-run/per-build assignment caps (the engine may already bound matrix
size — verify and surface), plus the org budget stop as the backstop.

### F3. Cloud-account quotas are invisible **[robustness]**

Providers have their own limits (AWS per-region vCPU quotas, instance-type
caps). Hitting them makes `Create` fail, which currently looks like generic
`no_capacity` (`provisioner.go:357`) — indistinguishable from spot scarcity, and
debounced away. The operator gets no signal that they've hit an *account* limit
they could request an increase for.

*Fix:* classify quota errors distinctly; surface "account quota reached, request
an increase" rather than silently retrying.

---

## G. Control loop & scheduling

### G1. Reactive-only control → overshoot / windup **[economics]**

The provisioner is a purely reactive controller: it reacts to *current* pending
demand. The `incoming = requested + provisioning` term (`provisioner.go:69`)
dampens double-counting of in-flight boots, but with long boot latency, pending
stays high across many ticks while machines boot — and if the dampening is
imperfect it overshoots, then everything finishes at once, then mass scale-down,
then the next spike repeats. No rate-limit on boots-per-tick, no predictive or
derivative term.

*Fix:* cap provisioning velocity per tick; consider a predictive term for
known-cyclical load (ties to the utilization-scaling track). Verify `incoming`
fully prevents windup under multi-minute boots.

### G2. Provision-time shape selection is naive **[soundness]**

`requirementsFromPool` quotes the pool's **default shape**
(`provisioner.go:371`), not a shape derived from the *actual* pending mix. If a
pool's default is 4-core and a 16-core step is pending, the provisioner boots
4-core machines the big step can never fit — **starvation** (the step stays
pending forever) unless the pool shape is manually set ≥ the largest step.
Conversely, booting big machines for many tiny steps wastes capacity.

*Fix:* derive the boot shape from pending demand (bin-pack at provision time),
or at minimum guarantee the pool shape covers the largest pending step and warn
otherwise.

### G3. Pool reconfiguration with live machines **[robustness]**

`runner.LoadAll` refreshes the registry every ~30s, so a `PATCH /runners`
(shape/policy/provider change) updates *future* decisions — but **existing
machines keep running under the old config**, and nothing drains or re-evaluates
them. Pool *deletion* with live machines is similarly unspecified: orphaned
machines, or a mass-terminate?

*Fix:* define config-change semantics — drain-and-replace vs let-drain-naturally
— and handle pool deletion explicitly (drain then remove).

---

## H. Operability

### H1. "Why is my build queued?" has no answer **[operability]**

The single most common CI support question. A run can be pending for many
reasons — `no_capacity`, `max_machines` hit, affinity-blocked on a full holder
(A2), provider down (C1), budget/quota exceeded (F1/F3) — and today there's no
per-run surface that says *which*. The ledger has some of it, but not joined to
"this run is waiting because…"

*Fix:* a per-run wait-reason, derived from the fleet state at pending time and
shown on the run page next to the Placement view.

### H2. Reclaim requeue vs the user's retry budget **[tension]**

A spot reclaim requeues a step (safe-by-default, PR 2/3). But does that requeue
consume the *user-configured* retry budget? It shouldn't — the step didn't fail,
the infrastructure did (Buildkite's `exit -1` vs `agent_stop` distinction). If
infra-requeues and user-retries share one counter, flaky spot silently eats the
retries meant for flaky tests.

*Fix:* separate "infrastructure retry" (reclaim/drain/lost) from "step retry"
(non-zero exit) at the fleet↔engine boundary; only the latter counts against the
user's `retry` limit.

### H3. Cost-ledger fidelity vs the actual cloud bill **[economics]**

The cost view is *estimated* (requested compute × wall-clock × rate). Real bills
diverge: boot time (billed, attributed to whom?), idle-between-steps on a warm
machine, per-second vs per-hour rounding, data-transfer (D5), and spot price
actually paid vs quoted. If the ledger's number drifts far from the invoice,
"explainable economics" loses trust.

*Fix:* reconcile estimated vs actual against provider cost APIs where available;
be explicit that the ledger is an *estimate* and show the basis; attribute boot
and idle time to the pool, not to runs.

---

## Priority read

Hard gate before advertising scale-out: **A1, A2** (provisioning correctness).
Crash-safety to land with the reconcile work: **A5**.
Decide before interruptible becomes a default: **B1** (workspace durability) and
**B4** (reclaim-window handling).
Verify now as possible live security bugs: **S2/S3/S4** (see
[security.md](./security.md)).
Ship before real money is at stake: **F1** (org budget/machine cap) — a cost
platform with no budget stop is a liability.
Everything else is real but sequenceable — see the [roadmap](./roadmap.md).

**33 hard cases** across soundness / tension / robustness / economics / clarity /
guardrails / control-loop / operability, plus **7 trust edges** in
[security.md](./security.md).
