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

## P1. Duplicate step execution — VERIFIED fenced at the engine **[verified]**

The failure mode: an agent claims a step (`running`), executes it, but its
`ReportStepComplete` is lost or its heartbeat lapses long enough to be marked
`lost`. `FailMachineAssignments` fails the assignment and the engine requeues it —
onto a **different machine that runs the same step again**, while the original
agent may still be running or have finished with side effects applied.

**Flint's internal state is fenced.** `CompleteStep` decodes an HMAC-signed task
token carrying `{WorkflowID, StepName, Attempt}` (`pg_engine.go:249`,
`token.go:18-38`), locks the step **for that exact attempt** (`LockStep`,
`pg_engine.go:267`), and **no-ops if it is already terminal**
(`pg_engine.go:280`). A retry gets a **new attempt + a fresh token**
(`loop.go:219`). So the original agent's late completion — after its attempt was
failed and requeued — cannot re-advance the workflow, overwrite the result, or
touch the new attempt. The fence I thought was missing already exists, via the
attempt-scoped signed token + terminal-idempotency.

**What is *not* fenced (and can't be): external side effects.** The original agent
already ran the deploy/publish/write; the requeued attempt runs it again. Flint
mediates *its own* state (workflow advancement, result acceptance) exactly-once,
but effects a step performs on the outside world are inherently at-least-once.

*Honest contract:* **Flint gives at-least-once step execution.** Exactly-once
holds for Flint-mediated state; steps with external side effects must be
idempotent (or fence themselves). The docs/UX must say this plainly rather than
imply exactly-once — that's the real action item here, not a missing fence.

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

## P3. `CompleteAssignment` — VERIFIED minor (stat drift only) **[verified]**

Better than feared. `FinishAssignment` **does** guard —
`WHERE ... status IN ('pending','assigned','running')`
(`step_assignments.sql:45`) — so a late complete after a `lost`/`failed`
assignment can't overwrite the terminal status, and the `busy → idle` transition
is guarded by a `status == busy` check (`claim.go`). The **only** unfenced part is
`IncrementMachineStepsCompleted`, which runs unconditionally regardless of whether
`FinishAssignment` affected a row (it's a `:exec`), so a duplicate
`ReportStepComplete` drifts the machine's completed-steps **stat**. A cosmetic
counter, not a soundness issue.

*Fix (small):* make `FinishAssignment` `:execrows` and only bump the stat when it
affected a row.

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

**Verified: the engine-state fence exists** (P1) and completion is effectively
idempotent (P3) — the scariest items are handled. So the real action items are
narrower:
- **The contract, not code:** make "at-least-once execution; idempotent steps for
  external side effects" explicit in docs/UX (P1). This is the honest framing and
  it's currently unstated.
- **P2** (reappearing-agent reconciliation) and **P6** (completion↔engine crash
  window) are the remaining genuine correctness gaps — real but lower-frequency.
- **P4/P5** are tuning (liveness false-positives vs stuck-work); **P3**'s stat
  drift and **P7**'s log dedup are minor.

Net: at-least-once delivery with an exactly-once *engine-state* fence. That's a
sound foundation for interruptible capacity — the reclaim→requeue path won't
corrupt workflow state; it just needs idempotent steps for external effects,
which must be said out loud.
