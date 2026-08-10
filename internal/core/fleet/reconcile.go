package fleet

import (
	"context"
	"errors"
	"strconv"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/pkg/compute"
)

// BootstrapEndpoints is what freshly created machines need to find Flint.
type BootstrapEndpoints struct {
	ServerGRPCURL    string
	ServerHTTPURL    string
	AgentDownloadURL string
}

// SetBootstrapEndpoints configures the bootstrap material handed to elastic
// machines at Create.
func (f *Fleet) SetBootstrapEndpoints(b BootstrapEndpoints) { f.bootstrap = b }

// Reconcile compares each elastic provider's actual inventory with the DB:
// instances Flint doesn't know about get destroyed (zombies cost money),
// machines the provider no longer runs go lost (spot reclaims, console
// deletions), and stuck terminating machines get their Destroy retried.
func (f *Fleet) Reconcile(ctx context.Context) error {
	q := db.New(f.pool)
	pools, err := q.ListMachinePools(ctx)
	if err != nil {
		return err
	}
	seenProviders := map[string]bool{}
	for _, pool := range pools {
		if pool.Provider == "static" || seenProviders[pool.Provider] {
			continue
		}
		seenProviders[pool.Provider] = true
		if err := f.reconcileProvider(ctx, pool.Provider); err != nil &&
			!errors.Is(err, compute.ErrNotReconcilable) {
			log.Error().Err(err).Str("provider", pool.Provider).Msg("fleet: reconcile failed")
		}
	}
	return nil
}

func (f *Fleet) reconcileProvider(ctx context.Context, providerName string) error {
	provider, err := f.provider(ctx, providerName)
	if err != nil {
		return err
	}
	refs, err := provider.List(ctx)
	if err != nil {
		return err
	}
	q := db.New(f.pool)

	// Index the DB's view of this provider's machines.
	dbMachines, err := q.ListProviderMachines(ctx, providerName)
	if err != nil {
		return err
	}
	dbByRef := map[string]db.ListProviderMachinesRow{}
	dbByMachineID := map[string]bool{} // every non-terminal machine Flint owns
	for _, m := range dbMachines {
		dbByMachineID[m.ID] = true
		if m.ProviderRef != nil {
			dbByRef[*m.ProviderRef] = m
		}
	}
	liveRefs := map[string]bool{}

	// Provider → DB: unknown live instances are zombies.
	for _, ref := range refs {
		if ref.State == compute.RefTerminated {
			continue
		}
		liveRefs[ref.ID] = true
		// Correlate on the durable MachineID tag FIRST. A machine Flint just
		// created is idle/requested with its provider_ref not yet committed, so
		// it isn't in dbByRef — but its instance is tagged with the MachineID,
		// which IS in the DB. Reaping on the ref alone would destroy a live
		// machine mid-provision.
		if ref.MachineID != "" && dbByMachineID[ref.MachineID] {
			f.clearZombie(ref.ID)
			continue
		}
		if _, known := dbByRef[ref.ID]; known {
			continue
		}
		// Genuinely unknown (no matching MachineID, no matching ref): give
		// in-flight Creates from a provider that can't surface a MachineID a
		// grace window, then reap. The grace is approximated by only reaping on a
		// second consecutive sighting (tracked in-memory).
		if !f.sightZombie(ref.ID) {
			continue
		}
		log.Warn().Str("provider", providerName).Str("ref", ref.ID).
			Msg("fleet: destroying zombie instance (provider-side, unknown to Flint)")
		if err := provider.Destroy(ctx, ref); err == nil {
			_, _ = q.InsertFleetDecision(ctx, db.InsertFleetDecisionParams{
				DecisionType: "reconcile_zombie",
				Inputs:       mustJSON(map[string]any{"provider": providerName, "ref": ref.ID, "action": "destroyed"}),
			})
			f.clearZombie(ref.ID)
		}
	}

	// DB → provider: active machines whose instance is gone are lost.
	for _, m := range dbMachines {
		switch m.Status {
		case machineIdle, machineBusy, machineDraining, machineProvisioning:
			if m.ProviderRef == nil || liveRefs[*m.ProviderRef] {
				continue
			}
			if err := f.markMachineLost(ctx, m.ID, m.Status, "provider reports instance gone"); err != nil {
				log.Error().Err(err).Str("machine", m.ID).Msg("fleet: mark lost failed")
			}
		case machineTerminating, machineLost:
			// Retry the destroy, then finish the lifecycle.
			ref := compute.MachineRef{Provider: providerName, ID: deref(m.ProviderRef)}
			if err := provider.Destroy(ctx, ref); err != nil {
				// Retried next pass. Logged because an instance that can never be
				// destroyed bills forever, and silence is what lets that run.
				log.Warn().Err(err).Str("machine", m.ID).Str("ref", ref.ID).
					Msg("fleet: destroy retry failed; machine still costs money")
				continue
			}
			tx, err := f.pool.Begin(ctx)
			if err != nil {
				log.Error().Err(err).Str("machine", m.ID).
					Msg("fleet: begin tx to finalize a destroyed machine failed")
				continue
			}
			from := m.Status
			if from == machineLost {
				// lost → terminating → terminated in one pass.
				qtx := db.New(f.pool).WithTx(tx)
				if err := transitionMachine(ctx, qtx, machineTransition{
					machineID: m.ID, from: machineLost, to: machineTerminating,
					eventType: "terminate", actor: actorSweep, reason: "lost machine cleanup",
				}); err != nil {
					_ = tx.Rollback(ctx)
					continue
				}
				from = machineTerminating
				if err := transitionMachine(ctx, qtx, machineTransition{
					machineID: m.ID, from: from, to: machineTerminated,
					eventType: "terminated", actor: actorProvider,
				}); err != nil {
					_ = tx.Rollback(ctx)
					continue
				}
				_ = tx.Commit(ctx)
				continue
			}
			qtx := db.New(f.pool).WithTx(tx)
			if err := transitionMachine(ctx, qtx, machineTransition{
				machineID: m.ID, from: from, to: machineTerminated,
				eventType: "terminated", actor: actorProvider,
			}); err != nil {
				_ = tx.Rollback(ctx)
				continue
			}
			_ = tx.Commit(ctx)
		}
	}
	return nil
}

// markMachineLost transitions a machine to lost and fails its assignments —
// the same path the heartbeat sweep takes, driven by provider truth instead.
func (f *Fleet) markMachineLost(ctx context.Context, machineID, fromStatus, reason string) error {
	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(f.pool).WithTx(tx)

	from := fromStatus
	if from == machineProvisioning {
		// A provisioning machine that vanished simply failed to boot.
		if err := transitionMachine(ctx, qtx, machineTransition{
			machineID: machineID, from: from, to: machineFailed,
			eventType: "boot_failed", actor: actorProvider, reason: reason,
		}); err != nil {
			return err
		}
		if err := qtx.ResolveFleetDecisionByMachine(ctx, db.ResolveFleetDecisionByMachineParams{
			MachineID: &machineID, DecisionType: "provision", Outcome: strp("instance_gone"),
		}); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	if err := transitionMachine(ctx, qtx, machineTransition{
		machineID: machineID, from: from, to: machineLost,
		eventType: "instance_gone", actor: actorProvider, reason: reason,
	}); err != nil {
		return err
	}
	errMsg := "machine lost: " + reason
	failed, err := qtx.FailMachineAssignments(ctx, db.FailMachineAssignmentsParams{
		MachineID: &machineID, Error: &errMsg,
	})
	if err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	f.failStepsViaSignals(ctx, failed)
	recordMachineLost(ctx, "instance_gone", 1)
	log.Warn().Str("machine", machineID).Str("reason", reason).
		Int("assignments", len(failed)).Msg("fleet: machine lost (provider truth)")
	return nil
}

// sightZombie returns true on the second consecutive sighting of an unknown
// ref (a cheap stand-in for instance age the provider doesn't report).
func (f *Fleet) sightZombie(refID string) bool {
	f.provMu.Lock()
	defer f.provMu.Unlock()
	if f.zombieSightings == nil {
		f.zombieSightings = map[string]int{}
	}
	f.zombieSightings[refID]++
	return f.zombieSightings[refID] >= 2
}

func (f *Fleet) clearZombie(refID string) {
	f.provMu.Lock()
	defer f.provMu.Unlock()
	delete(f.zombieSightings, refID)
}

// numericFromFloat converts a float into a pgtype.Numeric.
func numericFromFloat(v float64) pgtype.Numeric {
	var n pgtype.Numeric
	_ = n.Scan(strconv.FormatFloat(v, 'f', -1, 64))
	return n
}
