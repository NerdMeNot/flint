# Fleet: artifact, cache & workspace storage economics

Status: analysis · Part of the [fleet design set](./README.md)

The other half of the economics story. The [economics model](./economics.md)
prices *compute*; but the single most-cited cost in real CI fleets is **data
movement** — Canva's "300+ hours of compute per build before any actual work
began" was cold caches and dependency downloads, not CPU. Elastic/interruptible
machines make this worse: every fresh machine pays the cold-fill toll again. This
doc maps where Flint's data lives, what it costs to move, and the sharp edges.

## What's built

- **Artifacts** — tar.zst archives in S3 under an `artifacts/` prefix, structured
  keys (`pkg/artifact/store.go`), Upload/Download, via the `wsfs.S3FS` bridge.
  Inter-job handoff (jobs are one-machine since the pivot; they hand off through
  S3).
- **Cache** — layered: a **local** tier (LRU eviction under a byte watermark,
  `pkg/cache/local.go:170`) backed by a **remote S3** tier shared across machines
  (`pkg/cache/layered.go`, `s3_bridge.go`).
- **Git mirror** — per-repo bare mirror on a persistent machine; first checkout
  cold-clones, later checkouts pay only the fetch delta; concurrent steps on the
  *same machine* serialize on a per-mirror **file lock** (`pkg/checkout/mirror.go:46`).

The warm-machine story is good: mirrors + local cache amortize across builds on a
persistent box — "the single biggest repeat-build win of persistent machines over
ephemeral pods" (`mirror.go:21`). The edges are all about what happens when the
machine *isn't* warm.

---

## St1. Cross-machine cold-fill stampede **[economics/robustness]**

The mirror flock (`mirror.go:46`) serializes concurrent checkouts **within one
machine** — but nothing coordinates *across* machines. A monorepo push that
provisions 200 fresh machines has all 200 cold-pull the same mirror / cache /
base artifacts from S3 **simultaneously**: a thundering herd of identical reads,
S3 request amplification, and 200 slow starts paying full cold cost at once. On
interruptible capacity (machines churning constantly) this is the *steady state*,
not a cold-start one-off.

*Fix:* cross-machine cold-fill coordination — a warm-snapshot/pre-baked image so
fresh machines start warm (the snapshot track; the real fix), and/or a
request-coalescing read tier (regional pull-through cache) so N cold machines
cause 1 origin read, not N.

## St2. S3 egress + cross-region is the hidden bill **[economics]**

Every cold machine pulls mirror + caches + artifact inputs from S3; **S3 egress
and cross-region transfer are billed per byte**, and CI dependency trees are
huge. Warm/affinity machines amortize this (mirror + local cache); interruptible
and fresh machines pay it **every time**. And placement makes it worse:
`Requirements.Regions` is a cost preference with no transfer model (hard-cases
D5), so a machine can land in a region far from the S3 bucket and pay
cross-region egress on every pull.

*Fix:* price cold-fill data transfer into provisioning decisions (a machine that
must cold-pull 5 GB cross-region is not "cheap" even on spot); co-locate compute
with the bucket region; regional cache mirrors. The ledger should attribute
data-transfer cost, not just compute — otherwise the "cheap spot" number lies.

## St3. Cache scoping & poisoning — security × economics **[tension]**

The remote S3 cache tier "shares across machines" (`layered.go:10`). Shared
across *what boundary*? If it's shared across **tenants/workspaces** and keyed
only by a content hash (lockfile hash), a malicious or compromised build can
**poison a cache key** that another tenant then restores — a supply-chain attack
through the cache. This is the storage-side of the multi-tenant trust problem
([security.md](./security.md) S1/S5).

*Fix:* scope the remote cache per-workspace by default (a shared global tier only
for verifiably-immutable content); consider signed/verified cache entries so a
restore can detect tampering. Sharing is an economics win but must be an explicit,
bounded trust decision — not the default blast radius.

## St4. No retention / lifecycle on S3 **[economics]**

The **local** cache evicts LRU under a watermark (`local.go:170`), but the grep
turns up **no TTL, lifecycle, or eviction** on S3 **artifacts** or the **remote
cache** tier. Unbounded growth = a storage bill that only ever climbs, and stale
cache entries that never expire.

*Fix:* artifact retention policy (per-pipeline TTL, S3 lifecycle rules) and a
remote-cache TTL/size cap. Make retention a visible, configurable line item — a
platform selling cost-legibility can't leak storage cost silently.

## St5. Artifact-handoff bandwidth is the price of scale-to-zero **[economics]**

Jobs hand off through S3 (up on one machine, down on the next). For large
workspaces that's a full up+down per job boundary — bandwidth, latency, and
compression CPU paid to *avoid* keeping a machine around. It's the right
trade for scale-to-zero, but it's not free, and it's invisible today.

*Fix:* measure and surface handoff volume; for hot chains, warmth affinity
already keeps successive steps on one machine (no handoff) — extend that so a
job's likely successor prefers the same machine when the handoff would be large.

## St6. Eviction & partial-write correctness **[robustness, minor]**

Local LRU eviction (`local.go:171` `evictFor`) runs to fit incoming bytes — can
it evict a key a *concurrent* running build is mid-restore from? And S3 multipart
uploads that fail partway can leave a corrupt/truncated archive that a later
Download unpacks. Cache/artifact reads need integrity checks (size/digest) so a
poisoned-by-truncation entry fails closed, not open.

*Fix:* pin in-use cache keys against eviction; write artifacts with a digest and
verify on download; atomic (write-temp-then-rename / complete-multipart-or-abort)
uploads.

---

## The tie to warm-boot

St1 and St2 are the economic case *for* the snapshot/warm-boot track (economics
roadmap): pre-baking caches and mirrors into the machine image or an EBS-snapshot
volume is what turns a cold interruptible machine from "pays 300 cache-hours" into
"starts warm." `rankOffers` already prices boot latency, so a warm-snapshot offer
wins naturally once providers can emit it — but the *storage* side (what to bake,
how fresh to keep it, what the snapshot storage costs) is this doc's problem, and
it's a real cost/freshness tradeoff, not a free win.

## Priority read

**St3** (cache scoping/poisoning) is a security issue hiding in an economics
feature — decide the trust boundary before the shared remote cache ships widely.
**St1/St2** (cold-fill stampede + egress) are the biggest *recurring* cost and
the case for warm-boot. **St4** (no S3 retention) is a silent bill that should be
capped early. The rest are correctness hardening.
