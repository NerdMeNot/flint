# Fleet: trust, isolation & secrets

Status: analysis · Part of the [fleet design set](./README.md)

A whole pillar the [economics model](./economics.md) and [hard cases](./hard-cases.md)
only touched (B2). Elastic, bin-packed, multi-tenant compute is a *security*
system as much as an economic one: the same machine may run code from different
runs, projects, and workspaces, and the fleet decides who lands where. This doc
maps the trust boundaries, what's already sound, and where the sharp edges are.

Grounded where possible; a few points are stated as **requirements to verify**
against the runtime (agentd) rather than asserted current behavior.

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
  only way to make the agent single-tenant, which some workloads will require.
- The task token must be **step-lifetime-scoped and unreplayable** (see S2).
- Sensitive workspaces should be schedulable to single-tenant machines by policy,
  not just by luck of packing.

### S2. Task-token lifetime & revocation **[requirement to verify]**

The task token gates secret and clone-token pulls. For the pull model to hold, it
must be: (a) valid only for its step's lifetime, (b) revoked/expired on
`ReportStepComplete`, and (c) bound to the step identity so it can't be replayed
to fetch a *different* step's secrets. If it's a long-TTL HMAC with no
step-completion revocation, a later step on the same warm machine — or a leaked
token — could re-pull.

*Action:* verify the token carries a short expiry + step binding and is rejected
after completion; if not, that's a security bug, not a nicety.

### S3. Namespace isolation completeness **[requirement to verify]**

Per-step **network** isolation (CNI netns) is documented. Secret env vars,
`/proc/<pid>/environ`, core dumps, and shared `/tmp` cross containers only if
**pid + mount + user** namespaces are *also* per-step. If steps share a pid or
mount namespace, one tenant's step can read another's env/files on the same host.

*Action:* confirm agentd gives each step its own pid, mount, and (ideally) user
namespace — not just network. If not, cross-tenant secret exposure is possible
even with the good pull model, because the leak happens *after* injection.

### S4. Workspace data remanence on warm machines **[soundness]**

Warm/bin-packed machines are reused across runs. A run's workspace (source,
build outputs, injected secret *files*, caches) sits on local disk. GC exists —
the agent reaps directories for runs that have reached a terminal state
(`heartbeat.go:102-105`, via `TerminalRunIDs`) — but it's **lazy** (heartbeat
cadence). Between a run finishing and the next GC sweep, a step from a *different
workspace* scheduled onto the same machine could read the stale workspace dir.

*Action:* cleanup must be **eager on run-terminal / before reuse**, not just
periodic; and secret *files* (as opposed to env) must be written to per-step
tmpfs that's unmounted at step end, never the shared workspace tree.

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

Verify now (possible live security bugs, not future features): **S2**
(task-token lifetime), **S3** (pid/mount ns isolation), **S4** (eager workspace
cleanup before reuse). Design before multi-tenant pools ship: **S1/S5**
(dedicated isolation + pool-usage RBAC). The good news is the core secret model
(S-intro) is sound — these are edges, but S2–S4 are the kind of edges that are
*bugs* if they're wrong, so they lead.
