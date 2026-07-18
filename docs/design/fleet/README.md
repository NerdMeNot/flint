# Fleet design

The design corpus for Flint's fleet manager — the piece that makes "bring your
own machines, but through the fleet manager" real, and the thing that most
differentiates Flint from Buildkite (which ships scaling *primitives* and lets
customers hand-roll the economics). Context:
[`../competitive-buildkite.md`](../competitive-buildkite.md).

The core bet: **efficiency and economics tilt the CI decision more than feature
experience does** — and the whole system stays *explainable* (every decision in
the `fleet_decisions` ledger) and *playable in your head* (a pool's behavior is a
pure function of its config).

## Read in this order

1. **[economics.md](./economics.md)** — the conceptual model. Two design
   principles (never override stated constraints; playable in your head), the
   capacity abstraction (**reliability classes `stable`/`interruptible`, not cloud
   product names like "spot"**), the two policy planes, and the provider
   extension seam. Start here.
2. **[hard-cases.md](./hard-cases.md)** — the adversarial stress-test (33 cases).
   Where the model breaks: provisioning races under scale-out, the collision
   between cheap *interruptible* capacity and the *local-workspace affinity
   guarantee*, GPU as its own sub-fleet, provider outages, the missing budget
   stop, control-loop overshoot, provision-time shape mismatch, "why is my build
   queued," and the abstraction leaks still lurking. Grounded in the current code.
3. **[security.md](./security.md)** — trust, isolation & secrets. The secret
   pull-model is sound (the machine never holds the master key); the edges are
   multi-tenant bin-packing concentrating trust in the shared agent, task-token
   lifetime, namespace-isolation completeness, workspace remanence, and
   pool-as-privilege-boundary. Some items are possible *live* bugs, not future
   work.
4. **[protocol.md](./protocol.md)** — agent↔control-plane failure modes. The
   distributed-systems core: at-least-once step delivery vs steps with side
   effects (duplicate execution, fencing), split-brain on partition, the
   liveness oracle's false positives, completion idempotency. Flint gives
   *at-least-once* execution; the honest exactly-once story is spelled out here.
5. **[storage.md](./storage.md)** — artifact/cache/workspace economics. The other
   half of the bill: cold-fill stampede across fresh machines, S3 egress +
   cross-region transfer, cache scoping/poisoning (a security issue inside an
   economics feature), and missing S3 retention. The economic case for warm-boot.
6. **[roadmap.md](./roadmap.md)** — the reprioritized phased plan. Provisioning
   correctness and interruptible-safety land *before* the spot-economics polish.

## One-line status

The fleet manager machinery exists and runs (transition chokepoint, bin-pack
scheduler with warmth affinity, Quote→rank→Create provisioner, decision ledger,
central scale-down, reconcile). The work ahead is (a) wiring the **policy** the
machinery already parses but ignores, (b) fixing **soundness** bugs that gate
scale-out, and (c) resolving the **tensions** cheap/interruptible capacity
introduces. Nothing here is built yet — these are proposals.
