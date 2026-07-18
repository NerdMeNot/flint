package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/internal/core/db"
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
	// Provisioning demand excludes work pinned by hard affinity to an existing
	// holder machine: a new machine can't take it (it must run on the holder), so
	// counting it would over-provision idle capacity.
	demand := map[string]int64{}
	if rows, err := q.PendingProvisioningDemand(ctx); err == nil {
		for _, r := range rows {
			demand[r.PoolID] = r.N
		}
	}

	booted := 0
	for _, pool := range pools {
		if pool.Provider == "static" {
			continue // static capacity joins by token; nothing to boot
		}
		n, err := f.provisionPool(ctx, pool, demand[pool.ID])
		if err != nil {
			log.Error().Err(err).Str("pool", pool.Name).Msg("fleet: provisioning failed")
			continue
		}
		booted += n
	}
	return booted, nil
}

// tryLockPool takes a session-scoped advisory lock that serializes provisioning
// for one pool across every dispatch-mode worker sharing this database. It is
// non-blocking: if another worker already holds it, (nil, false) is returned and
// the caller skips this pool this tick rather than reading the same deficit and
// double-provisioning it. The returned release frees the lock and the connection
// and must be deferred when locked is true. Session-scoped (not xact-scoped)
// because provisioning spans slow provider Quote/Create calls that must not run
// inside a transaction; a crashed worker's dropped connection auto-releases the
// lock. Keyed the same way as the engine's per-org lock (orgs.sql), a distinct
// 'flint-prov:' namespace so the two never collide.
func (f *Fleet) tryLockPool(ctx context.Context, poolID string) (release func(), locked bool, err error) {
	conn, err := f.pool.Acquire(ctx)
	if err != nil {
		return nil, false, err
	}
	var got bool
	if err := conn.QueryRow(ctx,
		`SELECT pg_try_advisory_lock(hashtext('flint-prov:' || $1))`, poolID,
	).Scan(&got); err != nil {
		conn.Release()
		return nil, false, err
	}
	if !got {
		conn.Release()
		return nil, false, nil
	}
	return func() {
		_, _ = conn.Exec(ctx, `SELECT pg_advisory_unlock(hashtext('flint-prov:' || $1))`, poolID)
		conn.Release()
	}, true, nil
}

func (f *Fleet) provisionPool(ctx context.Context, pool db.ListMachinePoolsRow, pending int64) (int, error) {
	// Serialize this pool's provisioning across workers: without it, two dispatch
	// replicas both read the same deficit and both boot it (N× over-provision,
	// past max_machines/min_warm). The loser skips this pool this tick.
	release, locked, err := f.tryLockPool(ctx, pool.ID)
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

	booted := 0
	for range needed {
		offers, err := provider.Quote(ctx, req)
		if err != nil {
			return booted, fmt.Errorf("quote: %w", err)
		}
		if len(offers) == 0 {
			f.recordNoCapacity(ctx, pool, req)
			break
		}
		rankOffers(offers, pool.Objective)
		if err := f.bootMachine(ctx, pool, provider, offers); err != nil {
			return booted, err
		}
		booted++
	}
	if booted > 0 {
		log.Info().Int("machines", booted).Str("pool", pool.Name).
			Int64("pending", pending).Msg("fleet: provisioning machines")
	}
	return booted, nil
}

// bootMachine executes one provision decision: ledger + machine row + token in
// one transaction, then the provider Create (idempotent per machine id, so a
// crash between commit and Create is retried safely by reconciliation).
func (f *Fleet) bootMachine(ctx context.Context, pool db.ListMachinePoolsRow, provider compute.Provider, offers []compute.Offer) error {
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
		"pool": pool.Name, "objective": pool.Objective,
		"capacityType": pool.CapacityType, "minWarm": pool.MinWarm,
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
		return err
	}
	return tx2.Commit(ctx)
}

// ScaleDown terminates idle elastic machines past their pool's idle TTL,
// keeping the warm minimum. Static machines are never terminated — Flint
// doesn't own their power button.
func (f *Fleet) ScaleDown(ctx context.Context) (int, error) {
	q := db.New(f.pool)
	candidates, err := q.ClaimIdleMachinesPastTTL(ctx)
	if err != nil || len(candidates) == 0 {
		return 0, err
	}

	// Per-pool idle counts + policy so the warm floor holds.
	terminated := 0
	idleByPool := map[string]int64{}
	poolInfo := map[string]db.GetMachinePoolByIDRow{}
	for _, c := range candidates {
		if _, ok := poolInfo[c.PoolID]; !ok {
			info, err := q.GetMachinePoolByID(ctx, c.PoolID)
			if err != nil {
				continue
			}
			poolInfo[c.PoolID] = info
			counts, _ := q.CountPoolMachinesByStatus(ctx, c.PoolID)
			for _, r := range counts {
				if r.Status == machineIdle {
					idleByPool[c.PoolID] = r.N
				}
			}
		}
	}

	for _, c := range candidates {
		pool, ok := poolInfo[c.PoolID]
		if !ok || pool.Provider == "static" {
			continue
		}
		if idleByPool[c.PoolID] <= int64(pool.MinWarm) {
			continue // the warm floor is the user's declared speed choice
		}
		if err := f.terminateMachine(ctx, c.ID, c.PoolID, "idle_ttl"); err != nil {
			log.Error().Err(err).Str("machine", c.ID).Msg("fleet: scale-down failed")
			continue
		}
		idleByPool[c.PoolID]--
		terminated++
	}
	if terminated > 0 {
		log.Info().Int("machines", terminated).Msg("fleet: idle machines scaled down")
	}
	return terminated, nil
}

// terminateMachine drives idle → terminating → Destroy → terminated with the
// terminate decision ledgered.
func (f *Fleet) terminateMachine(ctx context.Context, machineID, poolID, reason string) error {
	q := db.New(f.pool)
	m, err := q.GetMachine(ctx, machineID)
	if err != nil {
		return err
	}

	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(f.pool).WithTx(tx)
	if err := transitionMachine(ctx, qtx, machineTransition{
		machineID: machineID, from: m.Status, to: machineTerminating,
		eventType: "terminate", actor: actorFleet, reason: reason,
		drainReason: &reason,
	}); err != nil {
		return err
	}
	if _, err := qtx.InsertFleetDecision(ctx, db.InsertFleetDecisionParams{
		PoolID: &poolID, MachineID: &machineID, DecisionType: "terminate",
		Inputs: mustJSON(map[string]any{"reason": reason, "idleSince": m.IdleSince, "steps": m.StepsCompleted}),
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
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

// rankOffers orders offers by the pool's objective:
//
//	cost     — cheapest first
//	latency  — fastest boot first
//	balanced — price × (1 + bootSeconds/300): a 5-minute boot doubles the
//	           effective price, so cheap-but-slow loses to slightly-pricier-
//	           but-fast unless the gap is real
func rankOffers(offers []compute.Offer, objective string) {
	score := func(o compute.Offer) float64 {
		switch objective {
		case "cost":
			return o.PricePerHourUSD
		case "latency":
			return float64(o.ExpectedBootSeconds)
		default:
			return o.PricePerHourUSD * (1 + float64(o.ExpectedBootSeconds)/300)
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
