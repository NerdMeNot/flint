package server

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/fleet"
	"github.com/NerdMeNot/flint/internal/core/runner"
	"github.com/NerdMeNot/flint/pkg/units"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/jackc/pgx/v5/pgtype"
)

// runnerPoolReq is the create/update body for a machine pool.
type runnerPoolReq struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Provider    string  `json:"provider"` // compute_providers.name; default "static"
	Arch        string  `json:"arch"`
	CPU         string  `json:"cpu"`
	Memory      string  `json:"memory"`
	Disk        string  `json:"disk"`
	GPU         *gpuReq `json:"gpu"`

	// Provider allow-lists narrowing what Quote may offer (elastic pools).
	InstanceTypes []string `json:"instanceTypes"`
	Regions       []string `json:"regions"`

	// Economics policy — the user states the tradeoff, the fleet optimizes
	// within it. Zero standing infra is simply minWarm=0.
	CapacityType   string                  `json:"capacityType"` // spot | on_demand | any
	Objective      string                  `json:"objective"`    // cost | latency | balanced
	MinWarm        int32                   `json:"minWarm"`
	MaxMachines    int32                   `json:"maxMachines"`
	IdleTTLSeconds int32                   `json:"idleTtlSeconds"`
	Overrides      []runner.PolicyOverride `json:"overrides"`

	// HourlyCost lets on-prem/static operators declare an amortized machine cost
	// so their pools feed the same economics pipeline as priced elastic offers.
	HourlyCost *float64 `json:"hourlyCost"`
}

type gpuReq struct {
	Vendor string `json:"vendor"`
	Model  string `json:"model"`
	Count  int32  `json:"count"`
}

// validatePoolReq structurally validates a pool request. Quantities must parse
// (loud rejection at the API, not a silent zero at dispatch).
func validatePoolReq(req *runnerPoolReq) string {
	if req.CPU != "" {
		if _, err := units.ParseCPUMillis(req.CPU); err != nil {
			return "invalid cpu quantity (use forms like 500m, 2, 4)"
		}
	}
	if req.Memory != "" {
		if _, err := units.ParseMemoryMB(req.Memory); err != nil {
			return "invalid memory quantity (use forms like 512Mi, 8Gi)"
		}
	}
	if req.Disk != "" {
		if _, err := units.ParseDiskGB(req.Disk); err != nil {
			return "invalid disk quantity (use forms like 50Gi, 100G)"
		}
	}
	switch req.CapacityType {
	case "", "spot", "on_demand", "any":
	default:
		return "capacityType must be one of: spot, on_demand, any"
	}
	switch req.Objective {
	case "", "cost", "latency", "balanced":
	default:
		return "objective must be one of: cost, latency, balanced"
	}
	if req.MinWarm < 0 || req.MaxMachines < 0 || req.IdleTTLSeconds < 0 {
		return "minWarm, maxMachines, and idleTtlSeconds must be non-negative"
	}
	if req.MaxMachines > 0 && req.MinWarm > req.MaxMachines {
		return "minWarm cannot exceed maxMachines"
	}
	return ""
}

// handleCreateRunner registers a machine pool via the API — the canonical
// create path (pools are DB/API-managed).
func (s *Server) handleCreateRunner(ctx context.Context, c *app.RequestContext) {
	var req runnerPoolReq
	if c.BindJSON(&req) != nil || strings.TrimSpace(req.Name) == "" {
		apiBadRequest(ctx, c, "name is required")
		return
	}
	if msg := validatePoolReq(&req); msg != "" {
		apiBadRequest(ctx, c, msg)
		return
	}
	if _, err := s.deps.Q.GetMachinePool(ctx, req.Name); err == nil {
		apiConflict(ctx, c, "a runner pool with this name already exists")
		return
	}
	// CPU/memory are optional — a pool that sets no defaults stamps no shape,
	// letting each job size itself.
	if err := s.deps.Q.UpsertMachinePool(ctx, upsertParamsFromReq(req)); err != nil {
		apiInternal(ctx, c, "failed to create runner pool")
		return
	}
	// Guarantee exactly one default exists: the first pool created becomes it.
	if n, err := s.deps.Q.CountDefaultMachinePools(ctx); err == nil && n == 0 {
		_ = s.deps.Q.SetDefaultMachinePool(ctx, req.Name)
	}
	c.JSON(consts.StatusCreated, utils.H{"name": req.Name})
}

// handleUpdateRunner edits a machine pool (full replace of the provided fields).
func (s *Server) handleUpdateRunner(ctx context.Context, c *app.RequestContext) {
	name := c.Param("name")
	if _, err := s.deps.Q.GetMachinePool(ctx, name); err != nil {
		apiNotFound(ctx, c, "runner pool not found")
		return
	}
	var req runnerPoolReq
	if c.BindJSON(&req) != nil {
		apiBadRequest(ctx, c, "invalid request body")
		return
	}
	req.Name = name // name is the immutable identity
	if msg := validatePoolReq(&req); msg != "" {
		apiBadRequest(ctx, c, msg)
		return
	}
	if err := s.deps.Q.UpsertMachinePool(ctx, upsertParamsFromReq(req)); err != nil {
		apiInternal(ctx, c, "failed to update runner pool")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"name": name})
}

// handleDeleteRunner removes a machine pool. The default pool is protected — you
// must promote another pool first, so a default always exists.
func (s *Server) handleDeleteRunner(ctx context.Context, c *app.RequestContext) {
	name := c.Param("name")
	row, err := s.deps.Q.GetMachinePool(ctx, name)
	if err != nil {
		apiNotFound(ctx, c, "runner pool not found")
		return
	}
	if row.IsDefault {
		apiBadRequest(ctx, c, "cannot delete the default pool — set another pool as default first")
		return
	}
	if err := s.deps.Q.DeleteMachinePool(ctx, name); err != nil {
		apiInternal(ctx, c, "failed to delete runner pool")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true})
}

// handleSetDefaultRunner promotes a pool to the default (and demotes the rest).
// Pipelines that set no runner: resolve to the default.
func (s *Server) handleSetDefaultRunner(ctx context.Context, c *app.RequestContext) {
	name := c.Param("name")
	if _, err := s.deps.Q.GetMachinePool(ctx, name); err != nil {
		apiNotFound(ctx, c, "runner pool not found")
		return
	}
	if err := s.deps.Q.SetDefaultMachinePool(ctx, name); err != nil {
		apiInternal(ctx, c, "failed to set default runner pool")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"name": name, "isDefault": true})
}

// handleMintPoolJoinToken mints (or rotates) the pool's agent join token.
// The plaintext is returned exactly once; only its hash is stored. Machines
// run `flint-agent --server <url> --token <this>` to join the pool.
func (s *Server) handleMintPoolJoinToken(ctx context.Context, c *app.RequestContext) {
	name := c.Param("name")
	if _, err := s.deps.Q.GetMachinePool(ctx, name); err != nil {
		apiNotFound(ctx, c, "runner pool not found")
		return
	}
	token, hash, err := fleet.MintToken()
	if err != nil {
		apiInternal(ctx, c, "failed to mint join token")
		return
	}
	if err := s.deps.Q.SetPoolJoinTokenHash(ctx, db.SetPoolJoinTokenHashParams{Name: name, JoinTokenHash: &hash}); err != nil {
		apiInternal(ctx, c, "failed to store join token")
		return
	}
	s.recordAudit(ctx, "runner_pool.join_token_rotated", "runner_pool")
	c.JSON(consts.StatusOK, utils.H{
		"pool":  name,
		"token": token,
		"note":  "shown once — rotating invalidates the previous token for NEW joins (already-registered machines keep their machine tokens)",
	})
}

func upsertParamsFromReq(req runnerPoolReq) db.UpsertMachinePoolParams {
	p := db.UpsertMachinePoolParams{
		Name:           req.Name,
		Provider:       orDefaultStr(req.Provider, "static"),
		Arch:           orDefaultStr(req.Arch, "amd64"),
		Cpu:            req.CPU,
		Memory:         req.Memory,
		InstanceTypes:  req.InstanceTypes,
		Regions:        req.Regions,
		CapacityType:   orDefaultStr(req.CapacityType, "on_demand"),
		Objective:      orDefaultStr(req.Objective, "balanced"),
		MinWarm:        req.MinWarm,
		MaxMachines:    req.MaxMachines,
		IdleTtlSeconds: req.IdleTTLSeconds,
	}
	if p.MaxMachines == 0 {
		p.MaxMachines = 10
	}
	if p.IdleTtlSeconds == 0 {
		p.IdleTtlSeconds = 900
	}
	if req.Description != "" {
		p.Description = &req.Description
	}
	if req.Disk != "" {
		p.Disk = &req.Disk
	}
	if req.GPU != nil && req.GPU.Vendor != "" {
		p.GpuVendor = &req.GPU.Vendor
		if req.GPU.Model != "" {
			p.GpuModel = &req.GPU.Model
		}
		count := req.GPU.Count
		if count == 0 {
			count = 1
		}
		p.GpuCount = pgtype.Int4{Int32: count, Valid: true}
	}
	if len(req.Overrides) > 0 {
		p.Overrides, _ = json.Marshal(req.Overrides)
	}
	if req.HourlyCost != nil {
		var n pgtype.Numeric
		if err := n.Scan(strconv.FormatFloat(*req.HourlyCost, 'f', -1, 64)); err == nil {
			p.HourlyCost = n
		}
	}
	return p
}

func orDefaultStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
