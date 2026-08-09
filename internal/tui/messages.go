package tui

import "time"

// Navigation messages.
type pushViewMsg struct{ view View }
type popViewMsg struct{}

// Data fetched messages.
type orgLoadedMsg struct {
	org *Org
	err error
}

type projectsLoadedMsg struct {
	projects []Project
	runs     map[string]*Run // latest run per project ID
	err      error
}

type runsLoadedMsg struct {
	projectID string
	runs      []Run
	err       error
}

type runDetailLoadedMsg struct {
	run   *Run
	state *WorkflowState
	err   error
}

// Periodic refresh.
type tickMsg time.Time

// Action results.
type runTriggeredMsg struct {
	run *Run
	err error
}

type gateApprovedMsg struct {
	err error
}

type gatesLoadedMsg struct {
	gates []PendingGate
	err   error
}

type machinesLoadedMsg struct {
	machines []Machine
	err      error
}

type machineDrainedMsg struct {
	err error
}
