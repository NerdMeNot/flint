package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/NerdMeNot/flint/internal/core/db"
)

// machineResponse is the fleet view of one machine, economics included: the
// user sees what each machine costs and what it has done — never a black box.
type machineResponse struct {
	ID           string  `json:"id"`
	PoolID       string  `json:"poolId"`
	Status       string  `json:"status"`
	Provider     string  `json:"provider"`
	ProviderRef  *string `json:"providerRef,omitempty"`
	InstanceType *string `json:"instanceType,omitempty"`
	Region       *string `json:"region,omitempty"`
	CapacityType *string `json:"capacityType,omitempty"`
	Hostname     *string `json:"hostname,omitempty"`
	Arch         string  `json:"arch"`
	CPUMillis    int64   `json:"cpuMillis"`
	MemoryMB     int64   `json:"memoryMb"`
	AgentVersion *string `json:"agentVersion,omitempty"`

	PricePerHourUSD *float64 `json:"pricePerHourUsd,omitempty"`
	CostToDateUSD   *float64 `json:"costToDateUsd,omitempty"`
	StepsCompleted  int32    `json:"stepsCompleted"`

	RequestedAt     string  `json:"requestedAt"`
	RegisteredAt    *string `json:"registeredAt,omitempty"`
	LastHeartbeatAt *string `json:"lastHeartbeatAt,omitempty"`
	IdleSince       *string `json:"idleSince,omitempty"`
	DrainReason     *string `json:"drainReason,omitempty"`
}

// handleListMachines returns the live fleet (filter by pool/status).
func (s *Server) handleListMachines(ctx context.Context, c *app.RequestContext) {
	lim := parsePagination(c).Limit
	off := listOffset(c)
	params := db.ListMachinesPagedParams{Limit: int32(lim), Offset: int32(off)}
	if pool := c.Query("pool"); pool != "" {
		params.PoolID = &pool
	}
	if status := c.Query("status"); status != "" {
		params.Status = &status
	}
	rows, err := s.deps.Q.ListMachinesPaged(ctx, params)
	if err != nil {
		apiInternal(ctx, c, "failed to list machines")
		return
	}
	out := make([]machineResponse, 0, len(rows))
	for _, m := range rows {
		out = append(out, machineToResponse(m))
	}
	paginatedResponse(c, out, PaginationResponse{NextCursor: nextOffsetCursor(off, lim, len(out))})
}

// handleGetMachine returns one machine with its event history.
func (s *Server) handleGetMachine(ctx context.Context, c *app.RequestContext) {
	m, err := s.deps.Q.GetMachine(ctx, c.Param("id"))
	if err != nil {
		apiNotFound(ctx, c, "machine not found")
		return
	}
	events, _ := s.deps.Q.ListMachineEvents(ctx, db.ListMachineEventsParams{
		MachineID: m.ID, Limit: 100,
	})
	type eventResponse struct {
		Type      string          `json:"type"`
		From      *string         `json:"from,omitempty"`
		To        *string         `json:"to,omitempty"`
		Actor     string          `json:"actor"`
		Reason    *string         `json:"reason,omitempty"`
		Metadata  json.RawMessage `json:"metadata,omitempty"`
		CreatedAt string          `json:"createdAt"`
	}
	evs := make([]eventResponse, 0, len(events))
	for _, e := range events {
		evs = append(evs, eventResponse{
			Type: e.EventType, From: e.FromStatus, To: e.ToStatus, Actor: e.Actor,
			Reason: e.Reason, Metadata: e.Metadata, CreatedAt: e.CreatedAt.Format(time.RFC3339),
		})
	}
	c.JSON(consts.StatusOK, utils.H{"machine": machineToResponse(m), "events": evs})
}

// handleDrainMachine asks a machine to finish its work and stop claiming —
// the operator's graceful off-ramp. Delivered via the next heartbeat.
func (s *Server) handleDrainMachine(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	m, err := s.deps.Q.GetMachine(ctx, id)
	if err != nil {
		apiNotFound(ctx, c, "machine not found")
		return
	}
	switch m.Status {
	case "idle", "busy":
	default:
		apiBadRequest(ctx, c, "machine is "+m.Status+" — only idle or busy machines can be drained")
		return
	}
	operator := "operator"
	if claims := claimsFromCtx(ctx); claims != nil && claims.Email != "" {
		operator = "operator:" + claims.Email
	}
	reason := "operator drain"
	if err := s.deps.Q.UpdateMachineStatus(ctx, db.UpdateMachineStatusParams{
		ID: id, Status: "draining", DrainReason: &reason,
	}); err != nil {
		apiInternal(ctx, c, "failed to drain machine")
		return
	}
	_ = s.deps.Q.InsertMachineEvent(ctx, db.InsertMachineEventParams{
		MachineID: id, EventType: "drain", FromStatus: &m.Status,
		ToStatus: strPointer("draining"), Actor: operator, Reason: &reason,
	})
	s.recordAudit(ctx, "machine.drain", "machine")
	c.JSON(consts.StatusOK, utils.H{"id": id, "status": "draining"})
}

// handleListDecisions is the economics ledger feed: what the fleet decided,
// what it chose, what it rejected, and how it turned out.
func (s *Server) handleListDecisions(ctx context.Context, c *app.RequestContext) {
	lim := parsePagination(c).Limit
	off := listOffset(c)
	params := db.ListFleetDecisionsParams{Limit: int32(lim), Offset: int32(off)}
	if pool := c.Query("pool"); pool != "" {
		params.PoolID = &pool
	}
	if dt := c.Query("type"); dt != "" {
		params.DecisionType = &dt
	}
	rows, err := s.deps.Q.ListFleetDecisions(ctx, params)
	if err != nil {
		apiInternal(ctx, c, "failed to list decisions")
		return
	}
	type decisionResponse struct {
		ID           string          `json:"id"`
		PoolID       *string         `json:"poolId,omitempty"`
		MachineID    *string         `json:"machineId,omitempty"`
		Type         string          `json:"type"`
		Inputs       json.RawMessage `json:"inputs"`
		Chosen       json.RawMessage `json:"chosen,omitempty"`
		Alternatives json.RawMessage `json:"alternatives,omitempty"`
		Outcome      *string         `json:"outcome,omitempty"`
		OutcomeMeta  json.RawMessage `json:"outcomeMetadata,omitempty"`
		CreatedAt    string          `json:"createdAt"`
	}
	out := make([]decisionResponse, 0, len(rows))
	for _, d := range rows {
		out = append(out, decisionResponse{
			ID: d.ID, PoolID: d.PoolID, MachineID: d.MachineID, Type: d.DecisionType,
			Inputs: d.Inputs, Chosen: d.Chosen, Alternatives: d.Alternatives,
			Outcome: d.Outcome, OutcomeMeta: d.OutcomeMetadata,
			CreatedAt: d.CreatedAt.Format(time.RFC3339),
		})
	}
	paginatedResponse(c, out, PaginationResponse{NextCursor: nextOffsetCursor(off, lim, len(out))})
}

// handleRunPlacement explains where each of a run's steps executed and at
// what price — the per-run half of decision transparency.
func (s *Server) handleRunPlacement(ctx context.Context, c *app.RequestContext) {
	rows, err := s.deps.Q.ListRunAssignments(ctx, c.Param("id"))
	if err != nil {
		apiInternal(ctx, c, "failed to load run placement")
		return
	}
	type placementResponse struct {
		StepName        string   `json:"stepName"`
		Attempt         int32    `json:"attempt"`
		Status          string   `json:"status"`
		MachineID       *string  `json:"machineId,omitempty"`
		InstanceType    *string  `json:"instanceType,omitempty"`
		CapacityType    *string  `json:"capacityType,omitempty"`
		PricePerHourUSD *float64 `json:"pricePerHourUsd,omitempty"`
		QueuedAt        string   `json:"queuedAt"`
		AssignedAt      *string  `json:"assignedAt,omitempty"`
		StartedAt       *string  `json:"startedAt,omitempty"`
		FinishedAt      *string  `json:"finishedAt,omitempty"`
		QueueWaitMs     *int64   `json:"queueWaitMs,omitempty"`
	}
	out := make([]placementResponse, 0, len(rows))
	for _, a := range rows {
		p := placementResponse{
			StepName: a.StepName, Attempt: a.Attempt, Status: a.Status,
			MachineID: a.MachineID, InstanceType: a.InstanceType, CapacityType: a.CapacityType,
			QueuedAt:   a.CreatedAt.Format(time.RFC3339),
			AssignedAt: fmtTimePtr(a.AssignedAt), StartedAt: fmtTimePtr(a.StartedAt),
			FinishedAt: fmtTimePtr(a.FinishedAt),
		}
		if f, err := a.PricePerHourUsd.Float64Value(); err == nil && f.Valid {
			v := f.Float64
			p.PricePerHourUSD = &v
		}
		if a.StartedAt != nil {
			wait := a.StartedAt.Sub(a.CreatedAt).Milliseconds()
			p.QueueWaitMs = &wait
		}
		out = append(out, p)
	}
	c.JSON(consts.StatusOK, utils.H{"placements": out})
}

func machineToResponse(m db.Machine) machineResponse {
	r := machineResponse{
		ID: m.ID, PoolID: m.PoolID, Status: m.Status, Provider: m.Provider,
		ProviderRef: m.ProviderRef, InstanceType: m.InstanceType, Region: m.Region,
		CapacityType: m.CapacityType, Hostname: m.Hostname, Arch: m.Arch,
		CPUMillis: m.CpuMillis, MemoryMB: m.MemoryMb, AgentVersion: m.AgentVersion,
		StepsCompleted:  m.StepsCompleted,
		RequestedAt:     m.RequestedAt.Format(time.RFC3339),
		RegisteredAt:    fmtTimePtr(m.RegisteredAt),
		LastHeartbeatAt: fmtTimePtr(m.LastHeartbeatAt),
		IdleSince:       fmtTimePtr(m.IdleSince),
		DrainReason:     m.DrainReason,
	}
	if f, err := m.PricePerHourUsd.Float64Value(); err == nil && f.Valid {
		price := f.Float64
		r.PricePerHourUSD = &price
		// Cost to date: price × wall-clock life. Terminated machines stop the
		// clock at termination.
		start := m.RequestedAt
		end := time.Now()
		if m.TerminatedAt != nil {
			end = *m.TerminatedAt
		}
		cost := price * end.Sub(start).Hours()
		r.CostToDateUSD = &cost
	}
	return r
}

func fmtTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format(time.RFC3339)
	return &s
}

func strPointer(s string) *string { return &s }
