# Fleet economics: the conceptual model

Status: proposed · Part of the [fleet design set](./README.md) · Depends on
`internal/core/fleet/` and `pkg/compute/`.

This doc is the **model**: the principles, the capacity abstraction, and the
extension seam. The [hard cases](./hard-cases.md) stress-test it; the
[roadmap](./roadmap.md) sequences the work. Background: the fleet manager is
Flint's differentiator (see [`../competitive-buildkite.md`](../competitive-buildkite.md))
— Buildkite ships scaling *primitives* and every serious customer hand-rolls the
economics (Canva's ~$1.8M/yr warm-boot savings, Hasura's Lambda scaler, Wix's
concurrency layer). The point is to make that story a product, not glue.

## Thesis

Experience (logs, timelines, annotations) is table stakes. **Efficiency and
economics are what tilt the decision.** A team evaluating Flint against
Buildkite + Elastic CI Stack is asking one question: "will this spend my compute
budget better, without me operating a fleet?" Every decision must land in the
`fleet_decisions` ledger so the answer is *explainable*, not a black box.

## Design principles

Two constraints bound every decision. They matter as much as the optimization.

**1. Never override the user's stated constraints.** The fleet manager optimizes
*within* what a pool declares — it never substitutes a less reliable capacity
class, a different instance, or a lower warm floor than the config asks for. A
`stable` pool never gets an interruptible/spot machine, full stop. `min_warm: 3`
means three machines stay alive forever — scale-down refuses to cross the floor
(`provisioner.go:279`, *"the warm floor is the user's declared speed choice"*),
and provisioning tops back up to it with zero pending work. A team that wants
three reliable boxes always on and doesn't care about the bill gets exactly that;
**$0 saved is the correct outcome when the config implies it.** `objective`
(cost/latency/balanced) is only a tie-breaker *among offers of the chosen class*
— it never reaches for a riskier one to save money.

**2. Playable in your head.** A pool's behavior must be a pure function of
`(pool config, branch, event)` — you can state what it does in one sentence
without simulating the optimizer or knowing what's running right now:

- **Overrides are ordered, first-match-wins** (like firewall/CSS rules), not
  merged or folded. Scan top-to-bottom, first `match` wins, that patch *is* the
  per-boot policy. One rule fires; you always know which.
- **Overrides govern only per-boot knobs — overflow capacity class and
  `objective`.** Those attribute cleanly to the run that triggered the boot.
  **Standing capacity (`min_warm`, `max_machines`, `idle_ttl`) stays pool-level**
  — an idle warm machine isn't owned by a branch. Per-branch standing capacity
  *reads* simple ("main: min_warm 3, PR: 0") but its real behavior needs you to
  simulate "which branches are active this second." So we don't offer it.
- **Boring defaults.** No overrides ⇒ base policy for every run.
- **The ledger is the backstop.** If reality and your mental model diverge, the
  `fleet_decisions` row names the override that fired and why — the model is
  *checkable*, not just claimed.

A pool you can say out loud: *"Keeps 3 warm stable machines; PR builds that
overflow the warm pool boot interruptible; everything releases after 10 min
idle."* That sentence is the whole config, and it's exactly what runs — on a
provider with no interruptible class, the last clause just reads "boot stable."

## Current state (what's actually built)

Real and running on a 1s loop (`loop.go:54`): expire boot deadlines → expire
heartbeat leases → release unclaimed → schedule → provision → scale down, with
reconcile every ~30s.

- **Transition chokepoint** — every machine state change flows through
  `transitionMachine()` (`transition.go:38`) against `allowedMachineTransitions`
  (`status.go:35`); invalid edges error.
- **Scheduler** — `schedulePool()` (`scheduler.go:55`); warmth affinity is a
  **hard guarantee** (`pickMachine`, `scheduler.go:133`): a run's holder machine
  is the only legal placement (workspace is a local dir); if full, the
  assignment waits rather than splitting the run. No-holder runs get best-fit by
  CPU.
- **Provisioner** — deficit = `pending − incoming + warmShort`, headroom-capped
  (`provisioner.go:67`); Quote → `rankOffers` by objective (`provisioner.go:404`)
  → boot `offers[0]` in one ledgered tx (`bootMachine`, `provisioner.go:120`).
- **Decision ledger** — `fleet_decisions` (`001_initial_schema.sql:647`), in-tx
  for provision/terminate/no_capacity/reconcile_zombie, outcomes backfilled.
- **Central scale-down** — `ScaleDown()` (`provisioner.go:247`), SKIP LOCKED,
  never `static`, holds the warm floor.
- **Reconciliation** — `Reconcile()` (`reconcile.go:30`) diffs provider `List` vs
  DB: zombies → Destroy (2nd sighting), vanished actives → lost.
- **Provider seam** — five-method `compute.Provider` (`compute.go:133`), priced
  `Offer`s, `database/sql`-style registry, three impls + a conformance kit.

The machinery is solid. The gaps (see [roadmap](./roadmap.md)) are in the
**policy** it reads and the **hard cases** (see [hard-cases](./hard-cases.md)) it
doesn't yet handle.

## Two policy planes

Overrides are per-branch/event, but provisioning is **pool-aggregate**
(`PendingAssignmentDemand` groups by `pool_id` only, `step_assignments.sql:112`;
`branch`/`event` live on `pipeline_runs`). Split knobs by what they physically
govern, and let overrides touch only the plane that's cleanly per-run:

- **Per-boot plane — overflow capacity class + `objective` (overridable).** Each
  boot is caused by specific pending work. Join the demand query to
  `pipeline_runs`, group by `(pool_id, branch, event)`, provision each group with
  its effective policy = base + **first matching override**.
  - *Accepted leak:* an idle machine booted for a PR can later run a `main` step
    (warmth affinity is per-run, not per-branch). Warm capacity is a pool
    resource; overrides control what *overflow* costs, not who may use the floor.
- **Standing-capacity plane — `min_warm`, `max_machines`, `idle_ttl` (pool-level,
  NOT overridable).** The deliberate ergonomic cut: per-branch standing capacity
  can't be reasoned about without simulating live activity.

"Keep main warm, PRs cheap" reads: `min_warm: N` + override
`{match:{event:pull_request}, set:{overflow: interruptible}}`. Resolution is one
pure function — the concrete form of "playable in your head":

```go
// internal/core/fleet/policy.go
type EffectivePolicy struct {
    Overflow  Class  // stable | interruptible | any
    Objective string
    Applied   string // the override that fired ("" = base), for the ledger
}
// Pure function of (base, branch, event) — no runtime state.
func resolvePolicy(base runner.Policy, branch, event string) EffectivePolicy
```

## Capacity model: reliability classes, not cloud product names

**"Spot" is an AWS word.** GCP calls it Spot VMs (née preemptible), Azure calls
it Spot, Fargate has Fargate Spot — and Hetzner, Fly, bare metal, a homelab,
static pools, and localdev have no such concept *at all*. Baking `spot |
on_demand` into the core leaks one cloud's catalog into every pool; a spec with
`capacity_type: spot` is nonsense pointed at Hetzner. The core speaks a
**universal property**; each provider translates.

That property is **interruptibility**: can the provider reclaim this machine out
from under a running step? It partitions every offer everywhere into two classes:

- **`stable`** — won't be reclaimed: on-demand (AWS/GCP/Azure), Hetzner cloud
  VMs, Fly machines, reserved, owned/bare-metal, localdev. Pricier/sunk cost.
- **`interruptible`** — may be reclaimed for price/capacity on short notice:
  AWS/GCP/Azure Spot, Fargate Spot, spot node pools. Cheaper, non-zero risk.

"spot" never appears in the core — just an AWS-labeled `interruptible` offer.
Offers still carry numeric `InterruptionRisk` (0..1): **class is the coarse
playable filter, risk is the honest tie-breaker.**

### Provider capability matrix

Each provider declares which classes it supplies; validation refuses a class a
provider doesn't offer.

| Provider | `stable` | `interruptible` | elastic? |
|---|---|---|---|
| AWS EC2 / GCP / Azure | ✓ on-demand | ✓ spot | yes |
| Fargate | ✓ | ✓ Fargate Spot | yes |
| Hetzner / Fly | ✓ | — | yes |
| k8s-as-provider | ✓ on-demand nodes | ✓ *iff* a spot node pool exists | yes |
| static / bare-metal / homelab | ✓ owned | — | **no** (join-only) |
| localdev | ✓ | — | yes |

### Base + overflow

Each pool sets a class per bucket the provisioner already computes (`needed =
pending − incoming + warmShort` — warm floor vs demand overflow):

- `capacity: { base: stable, overflow: interruptible }` — "reliable floor, cheap
  burst," the recommended cloud template.
- scalar `capacity: stable` = base = overflow (shorthand).
- `base` is the standing floor ⇒ pool-level; `overflow` ⇒ branch-overridable.

### Degradation rules — the whole point

1. **A class a provider lacks falls back toward *more* reliability, never less.**
   `overflow: interruptible` on Hetzner (or AWS when spot is dry) → runs
   `stable`, ledgered with the price delta. Work keeps moving.
2. **Fallback is asymmetric.** `interruptible → stable` allowed; **`stable →
   interruptible` forbidden** — a stable pool never silently gets something
   reclaimable; if stable is unavailable it *waits*. Principle 1 with teeth.
3. **Fallback is default, not mandate.** `strict: true` opts a bucket out (wait
   rather than pay more).
4. **Non-elastic (static/owned) pools ignore capacity policy** — nothing to
   Quote/Create (`Provision`/`ScaleDown` skip `static`, `provisioner.go:41,276`).

### `min_warm` on interruptible = best-effort

`min_warm: 3` on `stable` = *three, guaranteed*. On `interruptible` = *target
three, best-effort* (reclaimed floor machines re-provision; dips expected). A
hard floor requires `base: stable` — another reason it's the default. (This is
also a durability concern, not just warmth — see
[hard-cases §interruptible-vs-affinity](./hard-cases.md).)

### The permutation space

| Intent | Config | On AWS/GCP/Azure | On Hetzner/Fly/metal |
|---|---|---|---|
| Owned/fixed | static provider | n/a (join-only) | the machines you plugged in |
| All-reliable | `capacity: stable`, `min_warm: N` | N warm + on-demand burst | N warm + fixed-price burst |
| Reliable floor, cheap burst | `base: stable, overflow: interruptible` | N on-demand + spot burst | **≡ all-reliable** (degrades) |
| All-cheap | `capacity: interruptible` | best-effort spot everywhere | **≡ all-reliable** (degrades) |
| Just get me compute | `capacity: any`, `objective: cost` | cheapest that runs | cheapest fixed-price |

Rows 3–4 **become row 2 automatically** on a provider without an interruptible
class — no error, no per-provider spec. That is the test the abstraction must
pass, and `stable`/`interruptible` passes it; `spot`/`on_demand` does not.

**Not** a primary knob: **percentage ratios** ("30% on-demand") — not playable
(you can't tell what a given machine is without a running tally). Deferred,
aggregate-only, ledger-audited if ever added.

### Interface consequence

`Requirements.Capacity` / `Offer.Capacity` move from `CapacityType
{spot|on_demand|any}` to a reliability class `{stable|interruptible|any}`;
providers gain `Classes() []Class`. Each provider owns the translation — awsec2:
on-demand↔`stable`, spot↔`interruptible`. **"spot" then lives only inside
`pkg/compute/awsec2`.** This is the general rule, not a one-off: **no cloud-isms
in the core** — placement, disk-type, and image want the same treatment (see
[hard-cases §abstraction-leaks](./hard-cases.md)). The seam that already does it
right is `runner/sizes.go` (abstract CPU/mem in core; provider maps to catalog).

## Adding your own fleet (the extension seam)

The most significant piece: a clean, small contract a third party satisfies
without understanding the internals.

1. **Implement `compute.Provider`** — five methods (`compute.go:133`): `Name`,
   `Quote`, `Create`, `Destroy`, `List`. The whole economics contribution is
   **`Quote`**: return priced `Offer`s (`PricePerHourUSD`, `ExpectedBootSeconds`,
   `InterruptionRisk`, class). The platform ranks and ledgers; a provider only
   prices honestly. Hard promise: after `Create` + a bootstrap token, a
   `flint-agent` registers before the boot deadline.
2. **Self-register** in `init()`: `compute.Register("mytype", factory)`.
3. **Blank-import** in `internal/boot/server.go` and
   `internal/platform/server/provider_handlers.go`.
4. **Certify** against `computetest.RunConformance` — passing it *is* "works with
   Flint."

Reference sizes: localdev ~255 lines drives the full elastic path; static
providers ~20 lines (`Quote`→nil, `Create`→`ErrUnsupported`,
`List`→`ErrNotReconcilable`).

**Honest caveat — no runtime plugins.** Registration is compile-time; adding a
fleet edits two blank-import lists and rebuilds. Go makes dynamic loading hard.
Model: **prebuilt fleets ship in-tree; third parties fork or upstream.** A
subprocess/gRPC provider boundary (Terraform-style) is a possible future if
external demand is real.

**Prebuilt-fleet roadmap:** ec2 (done) → hetzner → k8s-as-provider → gce/azure →
fargate → static/localdev (done) → macos later. Each is "a `compute.Provider` + a
curated image + a sane default pool policy," not a CloudFormation stack per queue.

## Explainability (non-negotiable)

Every decision path writes `fleet_decisions` with enough `inputs` to reconstruct
the *why*: effective policy, which override fired, branch/event, rejected
alternatives (top-5, `provisioner.go:162`), backfilled outcome. The run page's
Placement + per-step Timeline (shipped) surface this; a pool-level economics
view is the natural follow-on. **If a decision can't be explained from the ledger
alone, the feature isn't done** — and cost attribution under bin-packing is a
current hole (see [hard-cases §economics-completeness](./hard-cases.md)).
