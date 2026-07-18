# Fleet: trust, isolation & secrets

Status: analysis · Part of the [fleet design set](./README.md)

A whole pillar the [economics model](./economics.md) and [hard cases](./hard-cases.md)
only touched (B2). Elastic, bin-packed, multi-tenant compute is a *security*
system as much as an economic one: the same machine may run code from different
runs, projects, and workspaces, and the fleet decides who lands where. This doc
maps the trust boundaries, what's already sound, and where the sharp edges are.

Grounded where possible; the "verify" items below were **verified against the
runtime** — see the status box.

---

## Verification status (verified — code-read of agentd/engine)

The three "possible live bug" flags came back **mostly reassuring**; the real
findings are narrower and more actionable than the original speculation:

| Item | Verdict | Evidence |
|---|---|---|
| **S2** task-token replay for secrets | **SAFE (premise was wrong)** — secrets are gated by machine-bearer token + assignment→machine binding + status∈{running,assigned}, *not* the task token; completion revokes via the status gate | `agentgrpc/data.go:25-27,88-99` |
| **S3** per-step pid/mount isolation | **SAFE for the /proc·mount·/tmp vector** — pid+mount+ipc+uts namespaces present (containerd default spec) | `runtime/containerd_linux.go:270`, containerd `defaultUnixNamespaces` |
| **S4** workspace remanence | **LOW** — GC is lazy (heartbeat-driven) but runs are runID-path-isolated + mount-ns-walled, so no same-path or cross-mount cross-tenant read | `agentd/daemon.go:215,282`, `state.go:56` |
| **P1/P3** duplicate completion | **FENCED at the engine** — attempt-scoped signed task token + terminal-idempotency no-op a superseded/duplicate completion | `pg_engine.go:249,267,280` |

**But S3 found two real hardening gaps** for genuinely mutually-distrusting
tenants (see S3 below), and S2 found one **latent gap** (`GetCloneToken`). Those
are the actual work; the speculative secret-leak is not present.

---

## What's already sound (don't regress it)

The secret-delivery design is good and worth stating so nobody weakens it:

- **Pull model, not push.** Secret *values* are never in the assignment payload
  or the database. The agent calls `GetStepSecrets` at execution time
  (`proto/agent/v1/agent.proto:46-64`), and the server resolves the mapping
  (name → store key) with environment → project → org scope fallback and returns
  plaintext over the gRPC data-plane.
- **The machine never holds the master key.** Envelope encryption
  (`pkg/secret/envelope.go`, per-secret DEKs) is decrypted **control-plane-side**;
  the same master key that unwraps provider credentials (`providers.go`) never
  leaves the control plane. A compromised machine cannot decrypt the store.
- **Per-step task token.** Data-plane pulls (secrets, clone token) are gated by a
  per-step HMAC **task token** (`agent.proto:146-148`), not the machine bearer
  token — so access is scoped to a specific step, not the whole machine.
- **Short-lived clone tokens.** Git credentials are minted on demand
  (`GetCloneToken`), not stored on the machine.

So the blast radius of a compromised machine is bounded to *the decrypted secrets
of the steps it is currently running* — not the master key, not the store, not
other machines. That's the right shape. The pressure below is on the edges of
*this* model, not a call to replace it.

---

## The sharp edges

### S1. The agent is a confused deputy on a multi-tenant host **[tension]**

Bin-packing puts steps from **different runs/workspaces** on one machine (B2),
and one `flint-agent` process fetches secrets for *all* of them, holding every
co-resident step's task token. Per-step container + CNI-network isolation exists,
but the agent process sits *above* the containers and is the single point that
can call `GetStepSecrets` for every tenant on the box. So the real cross-tenant
blast radius isn't the master key — it's **the shared agent's aggregate access to
every co-resident step's secrets**, plus any container-escape that reaches it.

*Implications:*
- A `isolation: dedicated` pool (B2) isn't just a bin-packing/cost knob — it's the
  only way to make the agent single-tenant, which untrusted multi-tenancy requires
  (reinforced by S3's hardening gaps below).
- Sensitive workspaces should be schedulable to single-tenant machines by policy,
  not just by luck of packing.

### S2. Data-plane auth — VERIFIED SAFE, one latent gap **[verified]**

Premise corrected: secret pulls are **not** task-token-gated. `GetStepSecrets`
authorizes with the machine bearer token + assignment→machine binding + a status
gate (`agentgrpc/data.go:25-27,88-99`), and secrets come from the assignment's
own payload scope — a machine can only read secrets for assignments legitimately
bound to it, and only while `running`/`assigned`. Completion flips the status,
so the gate is an **effective revoke-on-complete**. The task token (used only by
`ReportStepComplete`) has no TTL and is never revoked, but per-attempt binding +
terminal-idempotency (P1) make replay a harmless no-op.

*Latent gap (fix before it bites):* `GetCloneToken` (`data.go:68-84`) lacks the
assignment-status check `GetStepSecrets` has — currently low-impact only because
it returns no real forge token yet. Add the status gate before forge tokens ship.
Optionally give the task token a short TTL as defense-in-depth.

### S3. Namespace isolation — VERIFIED present, but container-grade **[verified]**

The specific vector (cross-tenant read via `/proc`, shared mounts, or `/tmp`) is
**blocked**: each step gets its own **pid + mount + ipc + uts** namespace from
containerd's default spec (`runtime/containerd_linux.go:270`, unpinned
`defaultUnixNamespaces` → runc unshares fresh), its own rootfs snapshot, and only
per-run `/workspace` + per-step `/flint/io` + per-step ro `/flint/secrets/env`
are bound in (`containerd_linux.go:481-499`). No host `/tmp`, no host `/proc`.

But it's **container-grade, not a hardened multi-tenant sandbox** — two real gaps:
- **No user namespace** (no `WithUserNamespace` anywhere): root-in-container =
  host root, so any kernel-level container escape collapses isolation across *all*
  co-resident tenants. The biggest gap.
- **Service-less steps share the HOST network namespace**
  (`oci.WithHostNamespace("network")`, `containerd_linux.go:217`) — not a
  `/proc`/file vector, but a genuine cross-tenant channel: steps can reach each
  other's localhost-bound ports and see host interfaces.
- (Plus: `privileged` steps are permitted by policy, `containerd_linux.go:264`,
  which defeats the isolation for that step.)

*Read:* fine for **cooperating tenants within one org** (the common case);
**not** a hard boundary for mutually-distrusting tenants — which is exactly why
`isolation: dedicated` (S1/B2) is the control there, and why user-ns + a per-step
netns option are the hardening backlog.

### S4. Workspace remanence — VERIFIED low **[verified]**

GC is confirmed **lazy** — the agent `os.RemoveAll`s a run's dir only when a
heartbeat response lists it (`daemon.go:215,282`, terminal runs via
`TerminalRunIDs`), on the ~10s cadence. **But it isn't a cross-tenant leak:** each
run gets its own `runs/<runID>/workspace` (`state.go:56`), so a later run can't
read another's by the same path, and S3's mount namespace walls off enumeration.
Residual is disk lingering + reliance on S3 holding. Still worth making cleanup
eager on run-terminal (defence in depth) and keeping secret *files* on per-step
tmpfs unmounted at step end — but this is hardening, not a live bug.

### S5. Pool as a privilege boundary — usage RBAC **[tension]** (expands B2)

A pool carries a provider + credentials + **network reach**. A pool wired into a
prod VPC (to reach a prod DB/registry) is a privilege: any pipeline that can name
it (`runner: prod-pool`) gets arbitrary code execution *inside that trust zone*.
Today pools are resolved by name from a global registry (`runner.LoadAll`) with
no check on *who* may target them.

*Fix:* pool-usage authorization through the existing Casbin RBAC + environment
scoping — which workspaces/environments may schedule onto which pools. A
prod-network pool should be targetable only by protected-environment pipelines.
The economics ledger should record the isolation/trust mode so cost differences
(dedicated vs shared) are explainable.

### S6. Bootstrap & join token exposure **[robustness]**

Elastic machines get a **one-time** bootstrap token (hashed server-side, cleared
on registration — good). But it travels in cloud-init/user-data, which is
readable by anything on the box via the instance metadata service and often via
the provider console. Static pools use a **long-lived shared join token** — a
much bigger standing risk (leak = anyone can register a machine into your pool
and receive step assignments + their secrets).

*Fix:* short bootstrap-token TTL tied to the boot deadline (mostly there via the
one-time + deadline design); for static pools, per-machine join tokens with
rotation and revocation, and IMDSv2-style hop-limit / metadata lockdown guidance
in the prebuilt images. A rogue machine registering into a pool is a secret-theft
vector — registration should be constrained (CIDR allowlists already exist on
cluster tokens in the competitive notes; apply here).

### S7. Data residency / compliance placement **[tension]**

Region is currently a free-form cost/preference input (`Requirements.Regions`).
For GDPR/data-residency, "this workload's data must not leave the EU" is a **hard
constraint**, not a cost tie-breaker — the optimizer must never place it out of
region to save money (a direct application of principle 1, but with legal teeth).

*Fix:* a pool-level residency constraint that is enforced as a hard filter on
offers and can't be overridden by objective/fallback; ledger the constraint so
compliance is auditable.

---

## Priority read

**Verified: no live secret-leak.** The three "possible bug" flags (S2/S3/S4)
came back safe for the vectors that would have been bugs — secrets aren't
task-token-gated, per-step pid/mount isolation is present, and lazy GC isn't
cross-tenant-readable. The core pull-model (S-intro) is sound.

The **real** work, now that the speculation is cleared:
- **S2 latent gap** — `GetCloneToken` needs the assignment-status check
  `GetStepSecrets` has, before real forge tokens ship. Small, do it early.
- **S3 hardening** — no user namespace + shared host network are the two things
  that make bin-packed multi-tenancy container-grade rather than a hard boundary.
  Fix (user-ns remap + a per-step netns option), or gate untrusted tenants behind
  **`isolation: dedicated`** (S1/S5) and be explicit that shared pools are for
  cooperating teams.
- **S5** — pool-usage RBAC before any pool has prod network reach.
- **S7** — residency as a hard constraint before EU/regulated workloads.
