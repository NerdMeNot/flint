package fleet

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// ErrMachineTransitionRaceLost is returned by transitionMachine when the row was
// no longer in the expected `from` state (another actor moved it first). It is a
// benign outcome for callers that don't hold the machine row locked — they skip
// the transition rather than clobber the concurrent one.
var ErrMachineTransitionRaceLost = errors.New("fleet: machine transition lost a race (row already moved)")

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
	n, err := qtx.UpdateMachineStatus(ctx, db.UpdateMachineStatusParams{
		ID:             t.machineID,
		FromStatus:     t.from,
		Status:         t.to,
		DrainReason:    t.drainReason,
		ProviderRef:    t.providerRef,
		BootDeadlineAt: t.bootDeadline,
	})
	if err != nil {
		return err
	}
	if n == 0 {
		// The row wasn't in `from` anymore — a concurrent actor moved it. Skip
		// the event too, so state and history stay in lockstep.
		return ErrMachineTransitionRaceLost
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
