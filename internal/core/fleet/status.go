// Package fleet owns the machine side of Flint: the machine lifecycle state
// machine, agent tokens, heartbeat leases, and (with the scheduler and
// provisioner) the economics decisions about which machines exist and what
// runs where. It deliberately clones the engine's proven patterns — a
// transition chokepoint validating every edge, an append-only event log
// written in the same transaction, and SKIP LOCKED sweeps for deadlines.
package fleet

import (
	"fmt"
	"slices"
)

// Machine statuses. A machine's life:
//
//	requested → provisioning → idle ⇄ busy → draining → terminating → terminated
//
// plus failed (boot never completed / Destroy gave up) and lost (heartbeat
// lease expired or the provider reports the instance gone).
const (
	machineRequested    = "requested"
	machineProvisioning = "provisioning"
	machineIdle         = "idle"
	machineBusy         = "busy"
	machineDraining     = "draining"
	machineTerminating  = "terminating"
	machineTerminated   = "terminated"
	machineFailed       = "failed"
	machineLost         = "lost"
)

// allowedMachineTransitions is the single source of truth for legal machine
// edges. Anything not listed is a bug surfacing as an error, never a silent
// state overwrite.
var allowedMachineTransitions = map[string][]string{
	// requested → idle covers the agent registering in the window between
	// provider.Create returning and the provisioning transition committing.
	machineRequested:    {machineProvisioning, machineIdle, machineFailed},
	machineProvisioning: {machineIdle, machineFailed, machineTerminating},
	machineIdle:         {machineBusy, machineDraining, machineTerminating, machineLost},
	machineBusy:         {machineIdle, machineDraining, machineLost},
	// draining → idle is the operator undrain path for static machines.
	machineDraining:    {machineTerminating, machineLost, machineIdle},
	machineTerminating: {machineTerminated, machineFailed},
	// lost → idle: a static machine's agent reappears; lost → terminating:
	// an elastic machine gets its Destroy attempt.
	machineLost: {machineIdle, machineTerminating},
	// terminated / failed are terminal (failed elastic machines still get a
	// best-effort Destroy via reconciliation, without leaving 'failed').
	machineTerminated: {},
	machineFailed:     {},
}

// validMachineTransition reports whether from → to is a legal edge.
func validMachineTransition(from, to string) bool {
	return slices.Contains(allowedMachineTransitions[from], to)
}

func invalidTransitionError(machineID, from, to string) error {
	return fmt.Errorf("fleet: invalid machine transition %s → %s for machine %s", from, to, machineID)
}
