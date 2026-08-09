package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/rs/zerolog/log"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// DefaultHeartbeatInterval is the cadence agents heartbeat at; the lease
// expires at 3× this, so a machine survives two missed beats.
const DefaultHeartbeatInterval = 10 * time.Second

// ErrBadRegistrationToken is returned when a registration token matches
// neither a provisioning machine's bootstrap token nor any pool join token.
var ErrBadRegistrationToken = errors.New("fleet: registration token not recognized")

// Registration is the agent's self-report at registration time.
type Registration struct {
	Token        string
	MachineID    string // set for elastic machines (pre-allocated); empty for static joins
	Hostname     string
	Arch         string
	OS           string
	CPUMillis    int64
	MemoryMB     int64
	DiskGB       int64
	AgentVersion string
	Labels       map[string]string
}

// RegisteredMachine is what the agent gets back: its identity and bearer token.
type RegisteredMachine struct {
	MachineID         string
	MachineToken      string // plaintext, shown once; only the hash is stored
	HeartbeatInterval time.Duration
	PoolName          string
}

// Register exchanges a bootstrap/join token for a machine identity. Elastic
// path: the token hash matches a provisioning machine's single-use bootstrap
// token (cleared here). Static path: the token matches a pool's join token
// and a new machine row is created directly in idle.
func (f *Fleet) Register(ctx context.Context, reg Registration) (RegisteredMachine, error) {
	tokenHash := HashToken(reg.Token)

	tx, err := f.pool.Begin(ctx)
	if err != nil {
		return RegisteredMachine{}, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	qtx := db.New(f.pool).WithTx(tx)

	machineToken, machineTokenHash, err := MintToken()
	if err != nil {
		return RegisteredMachine{}, err
	}
	expiresAt := time.Now().Add(3 * DefaultHeartbeatInterval)
	labelsJSON, _ := json.Marshal(reg.Labels)

	// Elastic path: single-use bootstrap token on a provisioning machine.
	m, err := qtx.GetMachineByBootstrapTokenHash(ctx, &tokenHash)
	switch {
	case err == nil:
		if err := qtx.CompleteMachineRegistration(ctx, db.CompleteMachineRegistrationParams{
			ID:                 m.ID,
			AgentTokenHash:     &machineTokenHash,
			CpuMillis:          reg.CPUMillis,
			MemoryMb:           reg.MemoryMB,
			DiskGb:             reg.DiskGB,
			Arch:               orDefault(reg.Arch, m.Arch),
			Os:                 orDefault(reg.OS, "linux"),
			Labels:             labelsJSON,
			Hostname:           optStr(reg.Hostname),
			AgentVersion:       optStr(reg.AgentVersion),
			HeartbeatExpiresAt: &expiresAt,
		}); err != nil {
			return RegisteredMachine{}, err
		}
		bootSeconds := int(time.Since(m.RequestedAt).Seconds())
		if err := transitionMachine(ctx, qtx, machineTransition{
			machineID: m.ID, from: m.Status, to: machineIdle,
			eventType: "registered", actor: actorAgent,
			metadata: map[string]any{"bootSeconds": bootSeconds, "hostname": reg.Hostname},
		}); err != nil {
			return RegisteredMachine{}, err
		}
		// Backfill the provision decision: the boot succeeded, and how long it
		// took — the observed data every boot-latency aid is built from.
		outcomeMeta, _ := json.Marshal(map[string]any{"bootSeconds": bootSeconds})
		if err := qtx.ResolveFleetDecisionByMachine(ctx, db.ResolveFleetDecisionByMachineParams{
			MachineID: &m.ID, DecisionType: "provision",
			Outcome: strp("boot_ok"), OutcomeMetadata: outcomeMeta,
		}); err != nil {
			return RegisteredMachine{}, err
		}
		pool, err := qtx.GetMachinePoolByID(ctx, m.PoolID)
		if err != nil {
			return RegisteredMachine{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return RegisteredMachine{}, err
		}
		// Measured, not estimated: this is the real boot latency the warm-vs-boot
		// trade is made against, whereas Offer.ExpectedBootSeconds is the provider's
		// claim about it.
		recordBootDuration(ctx, pool.Name, deref(m.CapacityType), time.Since(m.RequestedAt))
		log.Info().Str("machine", m.ID).Str("pool", pool.Name).Int("bootSeconds", bootSeconds).
			Msg("fleet: elastic machine registered")
		return RegisteredMachine{
			MachineID: m.ID, MachineToken: machineToken,
			HeartbeatInterval: DefaultHeartbeatInterval, PoolName: pool.Name,
		}, nil

	case !errors.Is(err, pgx.ErrNoRows):
		return RegisteredMachine{}, err
	}

	// Static path: pool join token.
	pool, err := qtx.GetPoolByJoinTokenHash(ctx, &tokenHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RegisteredMachine{}, ErrBadRegistrationToken
		}
		return RegisteredMachine{}, err
	}
	machineID, err := qtx.InsertStaticMachine(ctx, db.InsertStaticMachineParams{
		PoolID:                   pool.ID,
		CpuMillis:                reg.CPUMillis,
		MemoryMb:                 reg.MemoryMB,
		DiskGb:                   reg.DiskGB,
		Arch:                     orDefault(reg.Arch, pool.Arch),
		Os:                       orDefault(reg.OS, "linux"),
		Labels:                   labelsJSON,
		Hostname:                 optStr(reg.Hostname),
		AgentVersion:             optStr(reg.AgentVersion),
		AgentTokenHash:           &machineTokenHash,
		HeartbeatIntervalSeconds: int32(DefaultHeartbeatInterval.Seconds()),
		HeartbeatExpiresAt:       &expiresAt,
	})
	if err != nil {
		return RegisteredMachine{}, err
	}
	if err := insertMachineEvent(ctx, qtx, machineTransition{
		machineID: machineID, to: machineIdle,
		eventType: "registered", actor: actorAgent,
		reason:   "static machine joined via pool token",
		metadata: map[string]any{"hostname": reg.Hostname, "pool": pool.Name},
	}); err != nil {
		return RegisteredMachine{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RegisteredMachine{}, err
	}
	log.Info().Str("machine", machineID).Str("pool", pool.Name).Str("hostname", reg.Hostname).
		Msg("fleet: static machine joined")
	return RegisteredMachine{
		MachineID: machineID, MachineToken: machineToken,
		HeartbeatInterval: DefaultHeartbeatInterval, PoolName: pool.Name,
	}, nil
}

// AuthenticateMachine resolves a bearer token to its live machine row.
func (f *Fleet) AuthenticateMachine(ctx context.Context, token string) (db.Machine, error) {
	hash := HashToken(token)
	return db.New(f.pool).GetMachineByAgentTokenHash(ctx, &hash)
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
