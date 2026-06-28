package server

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/runner"
	"github.com/NerdMeNot/flint/internal/core/runner/render"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/jackc/pgx/v5/pgtype"
	corev1 "k8s.io/api/core/v1"
)

// runnerPoolReq is the create/update body for a runner pool (reference mode).
// Managed-mode fields (capacity envelope / scale-to-zero) come in Phase 2.
type runnerPoolReq struct {
	Name           string              `json:"name"`
	Description    string              `json:"description"`
	CPU            string              `json:"cpu"`
	Memory         string              `json:"memory"`
	Arch           string              `json:"arch"`
	GPU            *gpuReq             `json:"gpu"`
	NodeSelector   map[string]string   `json:"nodeSelector"`
	Tolerations    []corev1.Toleration `json:"tolerations"`
	ServiceAccount string              `json:"serviceAccount"`
	Workspace      *workspaceReq       `json:"workspace"`
	RunAsNonRoot   bool                `json:"runAsNonRoot"`
	// Mode: "reference" (default) or "managed". Managed pools carry a managed spec
	// and Flint owns their nodeSelector/toleration (derived from the pool name).
	Mode    string              `json:"mode"`
	Managed *runner.ManagedSpec `json:"managed"`
}

type gpuReq struct {
	Vendor string `json:"vendor"`
	Model  string `json:"model"`
	Count  int32  `json:"count"`
}

type workspaceReq struct {
	Mode         string `json:"mode"`         // agent | pvc | s3
	StorageClass string `json:"storageClass"` // required when mode=pvc
	Size         string `json:"size"`
}

// handleCreateRunner registers a runner pool via the API — the canonical create
// path (no CRD). Reference mode: the pool carries selectors/tolerations/resources
// that target existing nodes.
func (s *Server) handleCreateRunner(ctx context.Context, c *app.RequestContext) {
	var req runnerPoolReq
	if c.BindJSON(&req) != nil || strings.TrimSpace(req.Name) == "" {
		apiBadRequest(ctx, c, "name is required")
		return
	}
	if msg := validateWorkspaceReq(req.Workspace); msg != "" {
		apiBadRequest(ctx, c, msg)
		return
	}
	if _, err := s.deps.Q.GetRunnerPool(ctx, req.Name); err == nil {
		apiConflict(ctx, c, "a runner pool with this name already exists")
		return
	}
	// CPU/memory are optional — a pool that sets no defaults stamps no requests,
	// letting each job size itself (or run best-effort).
	if err := s.deps.Q.UpsertRunnerPool(ctx, upsertParamsFromReq(req)); err != nil {
		apiInternal(ctx, c, "failed to create runner pool")
		return
	}
	// Guarantee exactly one default exists: the first pool created becomes it.
	if n, err := s.deps.Q.CountDefaultRunnerPools(ctx); err == nil && n == 0 {
		_ = s.deps.Q.SetDefaultRunnerPool(ctx, req.Name)
	}
	c.JSON(consts.StatusCreated, utils.H{"name": req.Name})
}

// handleUpdateRunner edits a runner pool (full replace of the provided fields).
func (s *Server) handleUpdateRunner(ctx context.Context, c *app.RequestContext) {
	name := c.Param("name")
	if _, err := s.deps.Q.GetRunnerPool(ctx, name); err != nil {
		apiNotFound(ctx, c, "runner pool not found")
		return
	}
	var req runnerPoolReq
	if c.BindJSON(&req) != nil {
		apiBadRequest(ctx, c, "invalid request body")
		return
	}
	req.Name = name // name is the immutable identity
	if msg := validateWorkspaceReq(req.Workspace); msg != "" {
		apiBadRequest(ctx, c, msg)
		return
	}
	if err := s.deps.Q.UpsertRunnerPool(ctx, upsertParamsFromReq(req)); err != nil {
		apiInternal(ctx, c, "failed to update runner pool")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"name": name})
}

// handleDeleteRunner removes a runner pool. The default pool is protected — you
// must promote another pool first, so a default always exists.
func (s *Server) handleDeleteRunner(ctx context.Context, c *app.RequestContext) {
	name := c.Param("name")
	row, err := s.deps.Q.GetRunnerPool(ctx, name)
	if err != nil {
		apiNotFound(ctx, c, "runner pool not found")
		return
	}
	if row.IsDefault {
		apiBadRequest(ctx, c, "cannot delete the default pool — set another pool as default first")
		return
	}
	if err := s.deps.Q.DeleteRunnerPool(ctx, name); err != nil {
		apiInternal(ctx, c, "failed to delete runner pool")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"success": true})
}

// handleSetDefaultRunner promotes a pool to the default (and demotes the rest).
// Pipelines that set no runner: resolve to the default.
func (s *Server) handleSetDefaultRunner(ctx context.Context, c *app.RequestContext) {
	name := c.Param("name")
	if _, err := s.deps.Q.GetRunnerPool(ctx, name); err != nil {
		apiNotFound(ctx, c, "runner pool not found")
		return
	}
	if err := s.deps.Q.SetDefaultRunnerPool(ctx, name); err != nil {
		apiInternal(ctx, c, "failed to set default runner pool")
		return
	}
	c.JSON(consts.StatusOK, utils.H{"name": name, "isDefault": true})
}

// handleRunnerManifests renders a managed pool's Karpenter NodePool + EC2NodeClass
// for the platform's GitOps to apply (render-to-GitOps — Flint never writes to the
// cluster). Reference pools have no manifests.
func (s *Server) handleRunnerManifests(ctx context.Context, c *app.RequestContext) {
	row, err := s.deps.Q.GetRunnerPool(ctx, c.Param("name"))
	if err != nil {
		apiNotFound(ctx, c, "runner pool not found")
		return
	}
	if row.Mode != "managed" {
		apiBadRequest(ctx, c, "manifests are only available for managed pools")
		return
	}
	var ms runner.ManagedSpec
	if len(row.ManagedSpec) > 0 {
		_ = json.Unmarshal(row.ManagedSpec, &ms)
	}
	in := render.Input{
		Name:    row.Name,
		Arch:    row.Arch,
		Managed: ms,
		Prov:    s.provisioningProfile(),
	}
	if row.GpuVendor != nil && *row.GpuVendor != "" {
		in.GPU = &runner.GPURequest{Vendor: *row.GpuVendor, Model: derefStr(row.GpuModel), Count: int(row.GpuCount.Int32)}
	}
	m, err := render.Render(in)
	if err != nil {
		apiBadRequest(ctx, c, err.Error())
		return
	}
	c.JSON(consts.StatusOK, utils.H{"nodePool": m.NodePool, "nodeClass": m.NodeClass, "combined": m.Combined()})
}

// handleProvisioningInfo reports whether managed (Karpenter) runner pools are
// available — i.e. whether an admin has configured the cluster provisioning
// profile. Flint never probes the cluster (render-to-GitOps), so a configured
// profile is the signal the platform supports managed provisioning. The editor
// uses this to enable/disable the managed-pool option.
func (s *Server) handleProvisioningInfo(ctx context.Context, c *app.RequestContext) {
	c.JSON(consts.StatusOK, utils.H{
		"configured": s.deps.Config.Provisioning.Configured(),
		// Karpenter on AWS is the only managed backend today; the renderer sits
		// behind a seam for Azure/GKE later.
		"cloud": "aws",
	})
}

// provisioningProfile maps the server config's provisioning block to the
// renderer's profile type.
func (s *Server) provisioningProfile() runner.ProvisioningProfile {
	p := s.deps.Config.Provisioning
	return runner.ProvisioningProfile{
		Role:                  p.Role,
		SubnetSelector:        p.SubnetTags(),
		SecurityGroupSelector: p.SecurityGroupTags(),
		AMIFamily:             p.AMIFamily,
	}
}

// validateWorkspaceReq structurally validates the workspace block. The deep
// StorageClass RWX-provisioner check needs a Kubernetes client and runs in the
// worker (runner.ValidateStorageClassRWX) — the API server has no k8s client.
func validateWorkspaceReq(w *workspaceReq) string {
	if w == nil {
		return ""
	}
	switch w.Mode {
	case "", "agent", "s3":
		return ""
	case "pvc":
		if strings.TrimSpace(w.StorageClass) == "" {
			return "workspace mode=pvc requires a storageClass that supports ReadWriteMany"
		}
		return ""
	default:
		return "workspace mode must be one of: agent, pvc, s3"
	}
}

func upsertParamsFromReq(req runnerPoolReq) db.UpsertRunnerPoolParams {
	p := db.UpsertRunnerPoolParams{
		Name:          req.Name,
		Cpu:           req.CPU,
		Memory:        req.Memory,
		Arch:          orDefaultStr(req.Arch, "amd64"),
		RunAsNonRoot:  req.RunAsNonRoot,
		WorkspaceMode: "agent",
		WorkspaceSize: "10Gi",
		Mode:          "reference",
	}
	if req.Description != "" {
		p.Description = &req.Description
	}
	if req.ServiceAccount != "" {
		p.ServiceAccountName = &req.ServiceAccount
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
	if len(req.NodeSelector) > 0 {
		p.NodeSelector, _ = json.Marshal(req.NodeSelector)
	}
	if len(req.Tolerations) > 0 {
		p.Tolerations, _ = json.Marshal(req.Tolerations)
	}
	// Managed mode: Flint owns the pool's scheduling — derive the nodeSelector +
	// toleration that match the Karpenter NodePool this pool renders, and persist
	// the managed intent. The engine then targets the pool exactly like a
	// reference pool (no mode branching at dispatch).
	if req.Mode == "managed" {
		p.Mode = "managed"
		ms := req.Managed
		if ms == nil {
			ms = &runner.ManagedSpec{}
		}
		withDefaults := ms.WithDefaults()
		p.ManagedSpec, _ = json.Marshal(withDefaults)
		p.NodeSelector, _ = json.Marshal(runner.PoolNodeSelector(req.Name))
		p.Tolerations, _ = json.Marshal([]corev1.Toleration{runner.PoolToleration(req.Name)})
	}
	if w := req.Workspace; w != nil {
		if w.Mode != "" {
			p.WorkspaceMode = w.Mode
		}
		if w.StorageClass != "" {
			p.WorkspaceStorageClass = &w.StorageClass
		}
		if w.Size != "" {
			p.WorkspaceSize = w.Size
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
