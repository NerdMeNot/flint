# Fleet: agent ↔ control-plane protocol failure modes

Status: analysis · Part of the [fleet design set](./README.md)

The distributed-systems core the [hard cases](./hard-cases.md) skipped: the
control plane assigns work to autonomous agents over an unreliable network, with
machines that can vanish, partition, pause, or come back from the dead. This is
where "at-least-once delivery" meets "steps with side effects," and it's the
part that's genuinely hard to get right. Grounded in the current protocol and
queries.

## The protocol, as it stands

RPCs (`proto/agent/v1/agent.proto:22-54`): `RegisterMachine`, `ClaimStep`,
`ExecuteStep` (bidi stream), `ReportStepComplete`, `Heartbeat`, plus the
task-token data-plane (`GetStepSecrets`, `GetCloneToken`).

A step's lifecycle is **two-phase claim + report**:

1. **Bind** — scheduler moves `pending → assigned`, stamping `machine_id` +
   `claim_deadline_at` (`step_assignments.sql` BindAssignment).
2. **Claim** — the agent's `ClaimStep` atomically takes `assigned → running`
   (`ClaimAssignmentForAgent`, `WHERE status='assigned'`).
3. **Execute** — the `ExecuteStep` stream carries logs/progress.
4. **Report** — `ReportStepComplete` → engine accepts the result →
   `CompleteAssignment` moves `running → terminal` (`claim.go:26-62`).

Three sweeps provide **at-least-once** recovery:
- `ReleaseUnclaimedAssignments` — `assigned` past `claim_deadline_at` → `pending`
  (machine gets a strike). SKIP LOCKED.
- `FailMachineAssignments` — machine `→ lost` (heartbeat lease expired,
  `fleet.go:90`) → every live assignment fails → engine requeues via retry.
- `StaleRunningAssignmentsForMachine` — `running` work the machine's heartbeat no
  longer claims, past a grace period → recovered.

At-least-once + steps with side effects is the whole problem below.

---

## P1. Duplicate step execution — the core hazard **[soundness]** ← the big one

The failure: an agent claims a step (`running`), executes it, but either its
`ReportStepComplete` is lost (partition) **or** its heartbeat lapses long enough
to be marked `lost`. `FailMachineAssignments` then fails the assignment and the
engine requeues it — onto a **different machine that runs the same step again**.
Meanwhile the original agent may still be running it, or may have finished with
side effects already applied. **A step that deploys, publishes, sends a
notification, or writes to a store runs those side effects twice.**

The recovery mechanisms are exactly what *causes* the double-run — they can't
tell "agent dead" from "agent unreachable but working."

*What's needed:* a **fencing token** (monotonic per assignment/attempt). The
engine/fleet accepts a `ReportStepComplete` (and the step's externally-visible
effects, where they route through Flint — artifact upload, status posting) only
from the *current* fence; a superseded agent's late completion is rejected. This
turns at-least-once *delivery* into at-most-once *effect* for anything Flint
mediates. Effects the step performs directly (a raw `kubectl apply`) can't be
fenced by Flint — those need step-level idempotency, and the docs must say so
plainly rather than imply exactly-once.

## P2. Split-brain: the reappearing agent **[soundness]**

A partition makes the control plane mark a live agent `lost` and requeue its
work. When the partition heals, the agent heartbeats again and takes
`lost → idle` ("reappeared", `heartbeat.go:55-60`) — but its assignments were
already failed and possibly re-run elsewhere, and its local disk still holds a
half-finished run's workspace. There's a transition for the machine coming back,
but **no reconciliation of the orphaned in-flight work it was carrying**: the
agent may still be streaming logs / about to report for a step the engine has
already re-dispatched.

*What's needed:* on `lost → idle`, reconcile the machine's claimed assignments
against their current state; a reappeared agent must be told "your step N was
reassigned — stop / discard," and its late reports fenced (P1). Pair with the
control-plane-downtime catch-up sweep (hard-cases C5).

## P3. `CompleteAssignment` is not idempotent **[correctness]**

`CompleteAssignment` (`claim.go:29`) calls `FinishAssignment` (status → terminal)
**and** `IncrementMachineStepsCompleted` unconditionally. A duplicate
`ReportStepComplete` — plausible under retries/partitions — double-counts the
machine's completed-steps stat and can re-run the `busy → idle` logic. The
terminal-status write is harmless-if-repeated, but the increment drifts and the
completion isn't guarded on "was actually running."

*Fix:* guard `FinishAssignment` on `WHERE status='running'` and only bump stats /
transition when that UPDATE affected a row — so a second complete is a no-op.

## P4. The liveness oracle has false positives **[robustness]**

`lost` is declared on ~3 min of heartbeat silence (map: lost within 60s of a 3
min lease). A stop-the-world GC pause, a slow network, NTP skew, or a briefly
overloaded agent trips it → the machine is declared dead while working → needless
requeue → P1 double-run. `StaleRunningAssignmentsForMachine`'s grace period is
the same knob on the running-step side.

*Fix:* treat the lease as tunable and conservative (favor false-negative over
false-positive, since a false-positive costs a duplicate run); consider a
"suspected" state that requeues only after a second confirmation, and back off
requeue for steps known to be side-effecting.

## P5. Claim-deadline race — handled, but state it **[robustness]**

Between `assigned` and the agent's `ClaimStep`, the deadline can expire and
`ReleaseUnclaimedAssignments` returns the row to `pending` for rebinding. This is
*safe* because both `ClaimAssignmentForAgent` and the release guard on
`status='assigned'` — the loser finds nothing. But the tuning matters: too tight
a `claim_deadline` on a slow-booting agent thrashes (assign → release → reassign);
the strike system must not blacklist a machine that's merely slow to start.

## P6. Completion ↔ engine-acceptance handoff **[soundness]**

`CompleteAssignment` runs "after the engine accepted the step result"
(`claim.go:26`) — two separate transactions (engine transition, then fleet
finalize). A crash *between* them leaves the step advanced in the engine but the
assignment still `running`, to be mopped up by `StaleRunningAssignmentsForMachine`
— or, worse, requeued. The fleet-completion and engine-transition need to be one
atomic fact or an idempotent, repl-safe two-step with a clear owner.

*Fix:* make the engine transition and assignment-finalize idempotent and
co-recoverable; on restart, a `running` assignment whose step is already terminal
in the engine should finalize, not requeue.

## P7. Log stream at-least-once / dedup **[correctness, minor]**

Logs flow over the `ExecuteStep` stream (`LogBatch`/`LogAck`,
`agent.proto:236-252`). On stream reconnect (agent restart, transient drop), are
re-sent batches deduped, and is ordering preserved? Duplicated or reordered log
lines are cosmetic but erode trust in the log view (and in the group-duration
math from the log-rendering work). `ReportStepComplete` is a *separate* unary RPC
(good — decoupled from the fragile stream), and carries a `LogDigest`
(`agent.proto:316`) that could anchor dedup.

*Fix:* sequence-number log batches; dedup on reconnect against the last acked
sequence; use the `LogDigest` to detect truncation.

---

## Priority read

**P1** (fencing to prevent duplicate side effects) is the one that matters most —
it's the difference between "at-least-once delivery" (fine) and "runs your deploy
twice" (not fine), and it's foundational for making interruptible capacity
(reclaims → requeues) safe at all. **P2/P3/P6** are the correctness cluster around
it. **P4/P5** are tuning that trades duplicate-runs against stuck-work. Honest
framing for users: Flint gives **at-least-once step execution**; exactly-once
holds only for effects Flint mediates and fences — everything else needs
idempotent steps.
