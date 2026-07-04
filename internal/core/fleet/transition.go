package fleet

import (
	"context"
	"encoding/json"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// Actors recorded on machine events.
const (
	actorFleet    = "fleet"
	actorAgent    = "agent"
	actorProvider = "provider"
	actorSweep    = "sweep"
)

// machineTransition describes one state-machine edge to apply.
type machineTransition struct {
	machineID string
	from, to  string
	eventType string
	actor     string // fleet | agent | provider | sweep | operator:<id>
	reason    string
	metadata  map[string]any

	// Optional row updates applied with the status change.
	providerRef  *string
	drainReason  *string
	bootDeadline *time.Time
}

// transitionMachine is the chokepoint every machine status change flows
// through: validate the edge, update the row (with status-specific timestamp
// semantics in the query), and append the machine_events entry — all on the
// caller's transaction, so state and history can never disagree.
func transitionMachine(ctx context.Context, qtx *db.Queries, t machineTransition) error {
	if !validMachineTransition(t.from, t.to) {
		return invalidTransitionError(t.machineID, t.from, t.to)
	}
	if err := qtx.UpdateMachineStatus(ctx, db.UpdateMachineStatusParams{
		ID:             t.machineID,
		Status:         t.to,
		DrainReason:    t.drainReason,
		ProviderRef:    t.providerRef,
		BootDeadlineAt: t.bootDeadline,
	}); err != nil {
		return err
	}
	return insertMachineEvent(ctx, qtx, t)
}

func insertMachineEvent(ctx context.Context, qtx *db.Queries, t machineTransition) error {
	var meta []byte
	if len(t.metadata) > 0 {
		meta, _ = json.Marshal(t.metadata)
	}
	actor := t.actor
	if actor == "" {
		actor = actorFleet
	}
	params := db.InsertMachineEventParams{
		MachineID: t.machineID,
		EventType: t.eventType,
		Actor:     actor,
		Metadata:  meta,
	}
	if t.from != "" {
		from := t.from
		params.FromStatus = &from
	}
	if t.to != "" {
		to := t.to
		params.ToStatus = &to
	}
	if t.reason != "" {
		reason := t.reason
		params.Reason = &reason
	}
	return qtx.InsertMachineEvent(ctx, params)
}
