package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/runner"
	"github.com/NerdMeNot/flint/pkg/compute"
	"github.com/NerdMeNot/flint/pkg/units"
)

// noCapacityDebounce bounds how often a pool's inability to provision is
// ledgered — once per window, not once per tick.
const noCapacityDebounce = 5 * time.Minute

// Provision boots machines for pools whose demand exceeds capacity: the
// deficit is unschedulable pending work plus the warm minimum, capped by the
// pool's machine ceiling. Every decision — chosen offer, ranked alternatives,
// and later its outcome — lands in the fleet_decisions ledger, because an
// economics engine the user can't audit is a black box.
func (f *Fleet) Provision(ctx context.Context) (int, error) {
	q := db.New(f.pool)
	pools, err := q.ListMachinePools(ctx)
	if err != nil {
		return 0, err
	}
	// Provisioning demand, split by the run's (branch, event) so the fleet can
	// resolve per-branch/event economics policy before quoting. Excludes work
	// pinned by hard affinity to an existing holder (a new machine can't take it),
	// which would otherwise over-provision idle capacity.
	//
	// This query failing must NOT degrade to "no demand": that reads as a quiet,
	// healthy fleet — every scale-to-zero pool is skipped by the gate below and
	// queued work waits forever with nothing logged. Fail the pass instead, so
	// the caller logs it and the next pass retries.
	rows, err := q.PendingProvisioningDemandByGroup(ctx)
	if err != nil {
		return 0, fmt.Errorf("pending provisioning demand: %w", err)
	}
	demandByPool := map[string][]db.PendingProvisioningDemandByGroupRow{}
	totalByPool := map[string]int64{}
	for _, r := range rows {
		demandByPool[r.PoolID] = append(demandByPool[r.PoolID], r)
		totalByPool[r.PoolID] += r.N
	}

	booted := 0
	for _, pool := range pools {
		if pool.Provider == "static" {
			continue // static capacity joins by token; nothing to boot
		}
		// Fixed-cost gate (#6): a scale-to-zero pool with no pending demand can
		// never need a boot, so skip the advisory lock + per-pool count entirely.
		// Without this, every idle pool pays a lock round-trip and a count query
		// each tick, so the loop's floor cost grows with pool count even when the
		// whole fleet is quiet. Pools with a warm floor (min_warm > 0) still fall
		// through — maintaining that floor inherently requires the current count.
		if totalByPool[pool.ID] == 0 && pool.MinWarm == 0 {
			continue
		}
		n, err := f.provisionPool(ctx, pool, totalByPool[pool.ID], demandByPool[pool.ID])
		if err != nil {
			log.Error().Err(err).Str("pool", pool.Name).Msg("fleet: provisioning failed")
			continue
		}
		booted += n
	}
	return booted, nil
}

// provisionContext is the resolved economics context for a pool's boots this
// tick: the reliability class and objective to quote by, and the (branch, event)
// that selected them (recorded in the ledger). CapacityType and Objective are
// resolved per the pool's dominant pending demand via ResolvePolicy; the warm
// floor stays pool-level (min_warm is standing capacity, not per-branch — a pool
// serves many branches at once, so there is no single "active" context for it).
type provisionContext struct {
	branch, event   string
	capacity        compute.CapacityType
	objective       string
	overrideApplied bool
}

// basePolicyFromRow reconstructs the pool's base economics policy (with its
// per-branch/event overrides parsed from the stored JSON) so ResolvePolicy can
// apply them. Invalid overrides JSON degrades to "no overrides" — the API
// validates on write, so this only guards hand-edited rows.
func basePolicyFromRow(pool db.ListMachinePoolsRow) runner.Policy {
	base := runner.Policy{
		CapacityType: pool.CapacityType,
		Objective:    pool.Objective,
		MinWarm:      int(pool.MinWarm),
		MaxMachines:  int(pool.MaxMachines),
		IdleTTL:      time.Duration(pool.IdleTtlSeconds) * time.Second,
	}
	if len(pool.Overrides) > 0 {
		_ = json.Unmarshal(pool.Overrides, &base.Overrides)
	}
	return base
}

// resolveProvisionContext picks the pool's dominant pending demand group (the
// largest (branch, event) bucket) and resolves the effective policy for it. With
// no demand (warm-floor-only boots), it resolves the base policy against the
// empty context.
func resolveProvisionContext(pool db.ListMachinePoolsRow, groups []db.PendingProvisioningDemandByGroupRow) provisionContext {
	var branch, event string
	var best int64
	for _, g := range groups {
		if g.N > best {
			best, branch, event = g.N, g.Branch, g.Event
		}
	}
	eff := runner.ResolvePolicy(basePolicyFromRow(pool), branch, event)
	return provisionContext{
		branch: branch, event: event,
		capacity:        compute.CapacityType(eff.CapacityType),
		objective:       eff.Objective,
		overrideApplied: eff.CapacityType != pool.CapacityType || eff.Objective != pool.Objective,
	}
}

// tryLockPool takes a session-scoped advisory lock that serializes one kind of
// capacity decision (namespace) for one pool across every dispatch-mode worker
// sharing this database. It is non-blocking: if another worker already holds it,
// (nil, false) is returned and the caller skips this pool this tick rather than
// racing on a stale read. The returned release frees the lock and the connection
// and must be deferred when locked is true. Session-scoped (not xact-scoped)
// because these paths span slow provider Create/Destroy calls that must not run
// inside a transaction; a crashed worker's dropped connection auto-releases the
// lock. Keyed like the engine's per-org lock (orgs.sql); each namespace
// ('flint-prov:', 'flint-scaledown:') is a distinct key so unrelated decisions
// don't serialize against each other.
func (f *Fleet) tryLockPool(ctx context.Context, namespace, poolID string) (release func(), locked bool, err error) {
	conn, err := f.pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	var got bool
	if err := conn.QueryRow(ctx,
		`SELECT pg_try_advisory_lock(hashtext($1 || $2))`, namespace, poolID,
	).Scan(&got); err != nil {
		conn.Release()
		return nil, false, err
	}
	if !got {
		conn.Release()
		return nil, false, nil
	}
	return func() {
		_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock(hashtext($1 || $2))`, namespace, poolID)
		conn.Release()
	}, true, nil
}

func (f *Fleet) provisionPool(ctx context.Context, pool db.ListMachinePoolsRow, pending int64, groups []db.PendingProvisioningDemandByGroupRow) (int, error) {
	// Serialize this pool's provisioning across workers: without it, two dispatch
	// replicas both read the same deficit and both boot it (N× over-provision,
	// past max_machines/min_warm). The loser skips this pool this tick.
	release, locked, err := f.tryLockPool(ctx, "flint-prov:", pool.ID)
	if err != nil {
		return 0, err
	}
	if !locked {
		return 0, nil
	}
	defer release()

	q := db.New(f.pool)
	counts := map[string]int64{}
	rows, err := q.CountPoolMachinesByStatus(ctx, pool.ID)
	if err != nil {
		return 0, err
	}
	var live int64
	for _, r := range rows {
		counts[r.Status] = r.N
		live += r.N
	}

	// Deficit: pending work the scheduler couldn't place (it ran this tick
	// before us) plus the warm floor, minus capacity already coming up.
	incoming := counts[machineRequested] + counts[machineProvisioning]
	warmShort := int64(pool.MinWarm) - counts[machineIdle] - incoming
	if warmShort < 0 {
		warmShort = 0
	}
	needed := pending - incoming + warmShort
	if pending-incoming < 0 {
		needed = warmShort
	}
	if headroom := int64(pool.MaxMachines) - live; needed > headroom {
		needed = headroom
	}
	if needed <= 0 {
		return 0, nil
	}

	provider, err := f.provider(ctx, pool.Provider)
	if err != nil {
		return 0, err
	}
	req, err := requirementsFromPool(pool)
	if err != nil {
		return 0, err
	}
	// Resolve this tick's economics from the pool's dominant pending demand:
	// per-branch/event policy sets the reliability class + objective (main →
	// stable, PRs → interruptible), recorded in the ledger.
	pc := resolveProvisionContext(pool, groups)
	req.Capacity = pc.capacity
	// Degrade the requested reliability class to one the provider actually
	// supplies (B1): a pool asking for interruptible on a stable-only provider
	// (a homelab, localdev) is upgraded to stable so it still boots, instead of
	// getting zero offers and silently starving the run.
	if degraded, changed := compute.DegradeCapacity(req.Capacity, provider.Classes()); changed {
		log.Info().Str("pool", pool.Name).
			Str("requested", string(req.Capacity)).Str("resolved", string(degraded)).
			Msg("fleet: reliability class degraded to provider-supported class")
		req.Capacity = degraded
		pc.capacity = degraded
	}

	booted := 0
	for range needed {
		offers, err := provider.Quote(ctx, req)
		if err != nil {
			return booted, fmt.Errorf("quote: %w", err)
		}
		// Drop offers whose quote validity has lapsed before we commit to one:
		// spot prices drift, and booting on a stale offer means the recorded
		// price (and the economics decision built on it) is fiction. An offer
		// that declares no expiry (zero value) is always valid.
		offers = freshOffers(offers, time.Now())
		if len(offers) == 0 {
			f.recordNoCapacity(ctx, pool, req)
			recordProvisionAttempt(ctx, pool.Name, string(pc.capacity), pc.objective, "no_capacity")
			break
		}
		rankOffers(offers, pc.objective)
		if err := f.bootMachine(ctx, pool, provider, offers, pc); err != nil {
			recordProvisionAttempt(ctx, pool.Name, string(pc.capacity), pc.objective, "create_failed")
			return booted, err
		}
		recordProvisionAttempt(ctx, pool.Name, string(pc.capacity), pc.objective, "booted")
		booted++
	}
	if booted > 0 {
		log.Info().Int("machines", booted).Str("pool", pool.Name).
			Int64("pending", pending).Str("branch", pc.branch).Str("event", pc.event).
			Str("capacity", string(pc.capacity)).Msg("fleet: provisioning machines")
	}
	return booted, nil
}

// bootMachine executes one provision decision: ledger + machine row + token in
// one transaction, then the provider Create (idempotent per machine id, so a
// crash between commit and Create is retried safely by reconciliation).
func (f *Fleet) bootMachine(ctx context.Context, pool db.ListMachinePoolsRow, provider compute.Provider, offers []compute.Offer, pc provisionContext) error {
	chosen := offers[0]
	bootstrapToken, tokenHash, err := MintToken()
	if err != nil {
		return err
	}

	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(f.pool).WithTx(tx)

	machineID, err := qtx.InsertMachine(ctx, db.InsertMachineParams{
		PoolID:   pool.ID,
		Provider: pool.Provider,
		InstanceType: func() *string {
			s := chosen.InstanceType
			return &s
		}(),
		Region:          optStr(chosen.Region),
		Zone:            optStr(chosen.Zone),
		CapacityType:    optStr(string(chosen.Capacity)),
		PricePerHourUsd: numericFromFloat(chosen.PricePerHourUSD),
		AcceptedOffer:   mustJSON(chosen),
		CpuMillis:       chosen.CPUMillis,
		MemoryMb:        chosen.MemoryMB,
		DiskGb:          chosen.DiskGB,
		Arch:            chosen.Arch,
		BootstrapTokenHash: func() *string {
			return &tokenHash
		}(),
	})
	if err != nil {
		return err
	}

	inputs := map[string]any{
		"pool": pool.Name, "objective": pc.objective,
		"capacityType": string(pc.capacity), "minWarm": pool.MinWarm,
		"branch": pc.branch, "event": pc.event, "overrideApplied": pc.overrideApplied,
	}
	alternatives := offers[1:min(len(offers), 6)] // top rejected offers, bounded
	if _, err := qtx.InsertFleetDecision(ctx, db.InsertFleetDecisionParams{
		PoolID: &pool.ID, MachineID: &machineID, DecisionType: "provision",
		Inputs: mustJSON(inputs), Chosen: mustJSON(chosen), Alternatives: mustJSON(alternatives),
	}); err != nil {
		return err
	}
	if err := insertMachineEvent(ctx, qtx, machineTransition{
		machineID: machineID, to: machineRequested, eventType: "requested",
		actor:    actorFleet,
		metadata: map[string]any{"instanceType": chosen.InstanceType, "pricePerHourUsd": chosen.PricePerHourUSD},
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// Provider call outside the tx (it takes seconds). Idempotent per
	// machineID: a crash here is retried by reconciliation.
	ref, err := provider.Create(ctx, chosen, compute.Bootstrap{
		ServerGRPCURL:     f.bootstrap.ServerGRPCURL,
		ServerHTTPURL:     f.bootstrap.ServerHTTPURL,
		MachineID:         machineID,
		RegistrationToken: bootstrapToken,
		PoolName:          pool.Name,
		AgentDownloadURL:  f.bootstrap.AgentDownloadURL,
	})

	tx2, err2 := f.pool.Begin(ctx)
	if err2 != nil {
		return err2
	}
	defer tx2.Rollback(ctx) //nolint:errcheck
	qtx2 := db.New(f.pool).WithTx(tx2)

	if err != nil {
		if terr := transitionMachine(ctx, qtx2, machineTransition{
			machineID: machineID, from: machineRequested, to: machineFailed,
			eventType: "create_failed", actor: actorProvider, reason: err.Error(),
		}); terr != nil {
			return terr
		}
		if rerr := qtx2.ResolveFleetDecisionByMachine(ctx, db.ResolveFleetDecisionByMachineParams{
			MachineID: &machineID, DecisionType: "provision", Outcome: strp("create_failed"),
			OutcomeMetadata: mustJSON(map[string]any{"error": err.Error()}),
		}); rerr != nil {
			return rerr
		}
		if cerr := tx2.Commit(ctx); cerr != nil {
			return cerr
		}
		return fmt.Errorf("create: %w", err)
	}

	// A very fast instance may have registered (requested → idle) before this
	// transition: then only the provider ref needs recording.
	current, err := qtx2.GetMachine(ctx, machineID)
	if err != nil {
		return err
	}
	if current.Status != machineRequested {
		if err := qtx2.SetMachineProviderRef(ctx, db.SetMachineProviderRefParams{
			ID: machineID, ProviderRef: &ref.ID,
		}); err != nil {
			return err
		}
		return tx2.Commit(ctx)
	}

	bootDeadline := time.Now().Add(bootDeadlineFor(chosen))
	if err := transitionMachine(ctx, qtx2, machineTransition{
		machineID: machineID, from: machineRequested, to: machineProvisioning,
		eventType: "provisioned", actor: actorProvider,
		providerRef: &ref.ID, bootDeadline: &bootDeadline,
		metadata: map[string]any{"providerRef": ref.ID},
	}); err != nil {
		if errors.Is(err, ErrMachineTransitionRaceLost) {
			// The instance registered (requested → idle) between the status read
			// above and here — the same fast-register case, just observed a beat
			// later. Record the provider ref and leave it idle; do NOT force it
			// back to provisioning (the pre-guard code silently did, moving a live
			// machine backwards).
			if err := qtx2.SetMachineProviderRef(ctx, db.SetMachineProviderRefParams{
				ID: machineID, ProviderRef: &ref.ID,
			}); err != nil {
				return err
			}
			return tx2.Commit(ctx)
		}
		return err
	}
	return tx2.Commit(ctx)
}

// ScaleDown terminates idle elastic machines past their pool's idle TTL,
// keeping the warm minimum. Static machines are never terminated — Flint
// doesn't own their power button.
func (f *Fleet) ScaleDown(ctx context.Context) (int, error) {
	candidates, err := db.New(f.pool).ClaimIdleMachinesPastTTL(ctx)
	if err != nil || len(candidates) == 0 {
		return 0, err
	}

	// Group idle candidates by pool, preserving first-seen order.
	byPool := map[string][]string{}
	order := []string{}
	for _, c := range candidates {
		if _, seen := byPool[c.PoolID]; !seen {
			order = append(order, c.PoolID)
		}
		byPool[c.PoolID] = append(byPool[c.PoolID], c.ID)
	}

	terminated := 0
	for _, poolID := range order {
		// Serialize a pool's scale-down across workers: without it, two replicas
		// each read an independent idle count and terminate down to minWarm
		// against their own view, collectively breaching the warm floor. The
		// loser skips this pool this tick.
		release, locked, err := f.tryLockPool(ctx, "flint-scaledown:", poolID)
		if err != nil {
			return terminated, err
		}
		if !locked {
			continue
		}
		terminated += f.scaleDownPool(ctx, poolID, byPool[poolID])
		release()
	}
	if terminated > 0 {
		log.Info().Int("machines", terminated).Msg("fleet: idle machines scaled down")
	}
	return terminated, nil
}

// scaleDownPool terminates a pool's idle-past-TTL machines down to its warm
// floor. Called under the pool's scale-down lock, so the idle count it reads
// already reflects any peer worker's terminations this tick — the minWarm floor
// is enforced against fresh, serialized state.
func (f *Fleet) scaleDownPool(ctx context.Context, poolID string, machineIDs []string) int {
	q := db.New(f.pool)
	pool, err := q.GetMachinePoolByID(ctx, poolID)
	if err != nil || pool.Provider == "static" {
		return 0 // static machines are never terminated — Flint doesn't own their power button
	}
	var idle int64
	counts, _ := q.CountPoolMachinesByStatus(ctx, poolID)
	for _, r := range counts {
		if r.Status == machineIdle {
			idle = r.N
		}
	}

	terminated := 0
	for _, id := range machineIDs {
		if idle <= int64(pool.MinWarm) {
			break // the warm floor is the user's declared speed choice
		}
		if err := f.terminateMachine(ctx, id, poolID, "idle_ttl"); err != nil {
			log.Error().Err(err).Str("machine", id).Msg("fleet: scale-down failed")
			continue
		}
		recordMachineTerminated(ctx, pool.Name, "idle_ttl")
		idle--
		terminated++
	}
	return terminated
}

// busyDriftGrace is how long a machine may read 'busy' with no live assignment
// before the drift sweep corrects it. Generous: the normal path commits the
// binding and the busy transition together, so anything caught here is a genuine
// accounting drift, and correcting one slowly costs nothing.
const busyDriftGrace = 2 * time.Minute

// ReconcileBusyDrift returns machines marked busy that hold no work to idle.
//
// busy → idle happens in exactly one place — CompleteAssignment, when the last
// assignment finishes — and nothing checked the result. A machine that missed it
// was stranded: scale-down only considers idle machines, and a healthy agent
// keeps renewing its lease so the heartbeat sweep never sees it either. It stayed
// busy, and it kept billing. ClaimIdleMachinesPastTTL already guards the opposite
// drift (idle while still holding work); this is the other half.
func (f *Fleet) ReconcileBusyDrift(ctx context.Context) (int, error) {
	stuck, err := db.New(f.pool).ClaimBusyMachinesWithoutWork(ctx, busyDriftGrace.Seconds())
	if err != nil || len(stuck) == 0 {
		return 0, err
	}
	fixed := 0
	for _, m := range stuck {
		if err := f.transitionInTx(ctx, machineTransition{
			machineID: m.ID, from: machineBusy, to: machineIdle,
			eventType: "idle", actor: actorSweep,
			reason: "machine held no live assignments",
		}); err != nil {
			if !errors.Is(err, ErrMachineTransitionRaceLost) {
				log.Error().Err(err).Str("machine", m.ID).
					Msg("fleet: correcting busy drift failed")
			}
			continue
		}
		fixed++
	}
	if fixed > 0 {
		log.Warn().Int("machines", fixed).
			Msg("fleet: returned machines marked busy with no work to idle")
	}
	return fixed, nil
}

// TerminateDrained finishes the job a drain started: a machine asked to drain
// that has no work left has done what was asked, so it is terminated.
//
// Nothing used to complete a drain. Scale-down only ever considers idle
// machines, so a drained machine sat in 'draining' — still billing — until its
// agent stopped heartbeating; the lease sweep then marked it 'lost' and
// reconciliation destroyed it. A graceful, operator-initiated action reached its
// end state through the failure path, minutes later, having paid for the wait.
//
// Static machines are excluded by the query: Flint doesn't own their power
// button, so 'draining' is where they rest until an operator acts.
func (f *Fleet) TerminateDrained(ctx context.Context) (int, error) {
	drained, err := db.New(f.pool).ClaimDrainedMachines(ctx)
	if err != nil || len(drained) == 0 {
		return 0, err
	}
	terminated := 0
	for _, m := range drained {
		if err := f.terminateMachine(ctx, m.ID, m.PoolID, "drained"); err != nil {
			log.Error().Err(err).Str("machine", m.ID).Msg("fleet: terminating drained machine failed")
			continue
		}
		recordMachineTerminated(ctx, m.PoolID, "drained")
		terminated++
	}
	if terminated > 0 {
		log.Info().Int("machines", terminated).Msg("fleet: drained machines terminated")
	}
	return terminated, nil
}

// terminateMachine drives idle → terminating → Destroy → terminated with the
// terminate decision ledgered.
func (f *Fleet) terminateMachine(ctx context.Context, machineID, poolID, reason string) error {
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(f.pool).WithTx(tx)

	// Read under lock, inside the transaction that transitions. Reading first and
	// transitioning after left a window where the status used as `from` was
	// already stale — the same shape as the heartbeat drain that silently dropped
	// spot-interruption notices. The optimistic guard caught it there, so this was
	// safe rather than wrong, but the guard should be the backstop, not the plan.
	m, err := qtx.LockMachine(ctx, machineID)
	if err != nil {
		return err
	}
	// Only capacity that is idle, drained, or already on its way out may be
	// terminated. A busy machine must be drained first — that is a deliberate
	// property of the transition table, and stating it here turns a future
	// "terminate now" caller into a clear error rather than a puzzling
	// illegal-transition one.
	if m.Status != machineIdle && m.Status != machineDraining && m.Status != machineProvisioning {
		return fmt.Errorf("fleet: machine %s is %s — drain it before terminating", machineID, m.Status)
	}
	if err := transitionMachine(ctx, qtx, machineTransition{
		machineID: machineID, from: m.Status, to: machineTerminating,
		eventType: "terminate", actor: actorFleet, reason: reason,
		drainReason: &reason,
	}); err != nil {
		if errors.Is(err, ErrMachineTransitionRaceLost) {
			// The machine is no longer in the state we read (it got bound to work,
			// or another actor is already terminating it) — abort rather than
			// destroy a machine that isn't idle anymore.
			return nil
		}
		return err
	}
	if _, err := qtx.InsertFleetDecision(ctx, db.InsertFleetDecisionParams{
		PoolID: &poolID, MachineID: &machineID, DecisionType: "terminate",
		Inputs: mustJSON(map[string]any{"reason": reason, "idleSince": m.IdleSince, "steps": m.StepsCompleted}),
	}); err != nil {
		return err
	}
	// Defense in depth: ClaimIdleMachinesPastTTL excludes machines with live work,
	// so this should be empty — but if a machine is ever terminated while holding
	// an assignment, fail it and route it back through the engine's retry path
	// rather than orphaning the step.
	killErr := "machine terminated (" + reason + ")"
	orphaned, err := qtx.FailMachineAssignments(ctx, db.FailMachineAssignmentsParams{
		MachineID: &machineID, Error: &killErr,
	})
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	if len(orphaned) > 0 {
		log.Warn().Str("machine", machineID).Int("assignments", len(orphaned)).
			Msg("fleet: terminated a machine that still held work — requeued via signals")
		f.failStepsViaSignals(ctx, orphaned)
	}

	provider, err := f.provider(ctx, m.Provider)
	if err != nil {
		return err
	}
	ref := compute.MachineRef{Provider: m.Provider, ID: deref(m.ProviderRef)}
	if err := provider.Destroy(ctx, ref); err != nil {
		// Stays 'terminating'; reconciliation retries the destroy.
		return fmt.Errorf("destroy: %w", err)
	}

	tx2, err := f.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx2.Rollback(ctx) //nolint:errcheck
	qtx2 := db.New(f.pool).WithTx(tx2)
	if err := transitionMachine(ctx, qtx2, machineTransition{
		machineID: machineID, from: machineTerminating, to: machineTerminated,
		eventType: "terminated", actor: actorProvider, reason: reason,
	}); err != nil {
		if errors.Is(err, ErrMachineTransitionRaceLost) {
			return nil // already finalized by reconcile's cleanup pass
		}
		return err
	}
	if err := qtx2.ResolveFleetDecisionByMachine(ctx, db.ResolveFleetDecisionByMachineParams{
		MachineID: &machineID, DecisionType: "terminate", Outcome: strp("terminated"),
	}); err != nil {
		return err
	}
	return tx2.Commit(ctx)
}

// recordNoCapacity ledgers a pool's provisioning dead-end, debounced.
func (f *Fleet) recordNoCapacity(ctx context.Context, pool db.ListMachinePoolsRow, req compute.Requirements) {
	q := db.New(f.pool)
	if last, err := q.LastNoCapacityDecision(ctx, &pool.ID); err == nil && time.Since(last) < noCapacityDebounce {
		return
	}
	_, _ = q.InsertFleetDecision(ctx, db.InsertFleetDecisionParams{
		PoolID: &pool.ID, DecisionType: "no_capacity",
		Inputs: mustJSON(map[string]any{"requirements": req, "provider": pool.Provider}),
	})
	log.Warn().Str("pool", pool.Name).Msg("fleet: provider returned no offers for pool requirements")
}

// requirementsFromPool maps a pool's shape and allow-lists to a Quote request.
func requirementsFromPool(pool db.ListMachinePoolsRow) (compute.Requirements, error) {
	req := compute.Requirements{
		Arch:          pool.Arch,
		Capacity:      compute.CapacityType(pool.CapacityType),
		InstanceTypes: pool.InstanceTypes,
		Regions:       pool.Regions,
	}
	// A pool that declares a GPU shape must have it surfaced in the Quote — else
	// the provider ranks CPU-only offers and boots a machine that can't run the
	// work. A vendor is the minimum meaningful declaration.
	if pool.GpuVendor != nil && *pool.GpuVendor != "" {
		gpu := &compute.GPU{Vendor: *pool.GpuVendor}
		if pool.GpuModel != nil {
			gpu.Model = *pool.GpuModel
		}
		if pool.GpuCount.Valid {
			gpu.Count = int(pool.GpuCount.Int32)
		}
		req.GPU = gpu
	}
	var err error
	if pool.Cpu != "" {
		if req.CPUMillis, err = units.ParseCPUMillis(pool.Cpu); err != nil {
			return req, err
		}
	}
	if pool.Memory != "" {
		if req.MemoryMB, err = units.ParseMemoryMB(pool.Memory); err != nil {
			return req, err
		}
	}
	if pool.Disk != nil && *pool.Disk != "" {
		if req.DiskGB, err = units.ParseDiskGB(*pool.Disk); err != nil {
			return req, err
		}
	}
	return req, nil
}

// freshOffers drops offers whose ExpiresAt has passed. A zero ExpiresAt means
// the provider makes no validity claim, so it's kept. Returns a filtered slice
// (the common case — nothing expired — returns the input unchanged).
func freshOffers(offers []compute.Offer, now time.Time) []compute.Offer {
	fresh := offers[:0:0]
	for _, o := range offers {
		if o.ExpiresAt.IsZero() || o.ExpiresAt.After(now) {
			fresh = append(fresh, o)
		}
	}
	return fresh
}

// rankOffers orders offers by the pool's objective:
//
//	cost     — cheapest first
//	latency  — fastest boot first
//	balanced — price × (1 + bootSeconds/300): a 5-minute boot doubles the
//	           effective price, so cheap-but-slow loses to slightly-pricier-
//	           but-fast unless the gap is real
//
// interruptionPenaltyWeight scales how much reclaim risk penalizes an offer's
// balanced score. At weight 1.0 a 100%-reclaim offer would double its effective
// price; a typical 5–10% spot risk adds a 5–10% premium — enough that a barely-
// cheaper-but-riskier spot offer loses to a stable one, but a genuinely cheap
// spot offer still wins.
const interruptionPenaltyWeight = 1.0

func rankOffers(offers []compute.Offer, objective string) {
	score := func(o compute.Offer) float64 {
		switch objective {
		case "cost":
			return o.PricePerHourUSD
		case "latency":
			return float64(o.ExpectedBootSeconds)
		default:
			// balanced: price penalized by boot latency AND reclaim risk. A cheap
			// spot offer that is likely to be reclaimed isn't the deal its sticker
			// price suggests — the reclaim costs a re-run — so fold InterruptionRisk
			// into the effective price (B3).
			return o.PricePerHourUSD *
				(1 + float64(o.ExpectedBootSeconds)/300) *
				(1 + o.InterruptionRisk*interruptionPenaltyWeight)
		}
	}
	sort.SliceStable(offers, func(i, j int) bool { return score(offers[i]) < score(offers[j]) })
}

// bootDeadlineFor bounds registration: 3× the expected boot, floor 5 minutes.
func bootDeadlineFor(offer compute.Offer) time.Duration {
	d := time.Duration(offer.ExpectedBootSeconds) * 3 * time.Second
	if d < 5*time.Minute {
		d = 5 * time.Minute
	}
	return d
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
