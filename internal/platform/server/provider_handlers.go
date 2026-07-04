package server

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/pkg/compute"
	// Provider types must be registered for validation and the test endpoint.
	_ "github.com/NerdMeNot/flint/pkg/compute/awsec2"
	_ "github.com/NerdMeNot/flint/pkg/compute/staticpool"
	"github.com/NerdMeNot/flint/pkg/secret"
)

// computeProviderReq is the create/update body for a compute provider.
// Credentials are accepted here, envelope-encrypted, and stored in the DB —
// never returned by the API. Most cloud providers should omit credentials and
// use the ambient chain (instance role / env / shared config).
type computeProviderReq struct {
	Name        string         `json:"name"`
	Type        string         `json:"type"` // registered compute type: static | aws
	Config      map[string]any `json:"config"`
	Credentials map[string]any `json:"credentials,omitempty"`
}

type computeProviderResponse struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Type           string         `json:"type"`
	Config         map[string]any `json:"config"`
	HasCredentials bool           `json:"hasCredentials"`
	CreatedAt      string         `json:"createdAt"`
}

func (s *Server) handleListComputeProviders(ctx context.Context, c *app.RequestContext) {
	rows, err := s.deps.Q.ListComputeProviders(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to list compute providers")
		return
	}
	out := make([]computeProviderResponse, 0, len(rows))
	for _, r := range rows {
		resp := computeProviderResponse{
			ID: r.ID, Name: r.Name, Type: r.ProviderType,
			HasCredentials: len(r.CredentialsEnc) > 0,
			CreatedAt:      r.CreatedAt.Format(time.RFC3339),
		}
		_ = json.Unmarshal(r.Config, &resp.Config)
		out = append(out, resp)
	}
	c.JSON(consts.StatusOK, utils.H{"providers": out})
}

func (s *Server) handleCreateComputeProvider(ctx context.Context, c *app.RequestContext) {
	var req computeProviderReq
	if c.BindJSON(&req) != nil || strings.TrimSpace(req.Name) == "" {
		apiBadRequest(ctx, c, "name is required")
		return
	}
	if !isRegisteredComputeType(req.Type) {
		apiBadRequest(ctx, c, "type must be a registered provider type: "+strings.Join(compute.Types(), ", "))
		return
	}
	if _, err := s.deps.Q.GetComputeProvider(ctx, req.Name); err == nil {
		apiConflict(ctx, c, "a compute provider with this name already exists")
		return
	}
	id, err := s.upsertComputeProvider(ctx, req)
	if err != nil {
		apiInternal(ctx, c, err.Error())
		return
	}
	s.recordAudit(ctx, "compute_provider.create", "compute_provider")
	c.JSON(consts.StatusCreated, utils.H{"id": id, "name": req.Name})
}

func (s *Server) handleUpdateComputeProvider(ctx context.Context, c *app.RequestContext) {
	name := c.Param("name")
	if _, err := s.deps.Q.GetComputeProvider(ctx, name); err != nil {
		apiNotFound(ctx, c, "compute provider not found")
		return
	}
	var req computeProviderReq
	if c.BindJSON(&req) != nil {
		apiBadRequest(ctx, c, "invalid request body")
		return
	}
	req.Name = name // name is the immutable identity (pools reference it)
	if !isRegisteredComputeType(req.Type) {
		apiBadRequest(ctx, c, "type must be a registered provider type: "+strings.Join(compute.Types(), ", "))
		return
	}
	id, err := s.upsertComputeProvider(ctx, req)
	if err != nil {
		apiInternal(ctx, c, err.Error())
		return
	}
	s.recordAudit(ctx, "compute_provider.update", "compute_provider")
	c.JSON(consts.StatusOK, utils.H{"id": id, "name": name})
}

func (s *Server) handleDeleteComputeProvider(ctx context.Context, c *app.RequestContext) {
	name := c.Param("name")
	row, err := s.deps.Q.GetComputeProvider(ctx, name)
	if err != nil {
		apiNotFound(ctx, c, "compute provider not found")
		return
	}
	// Refuse to orphan pools that reference this provider.
	pools, err := s.deps.Q.ListMachinePools(ctx)
	if err == nil {
		for _, p := range pools {
			if p.Provider == name {
				apiBadRequest(ctx, c, "pool "+p.Name+" references this provider — repoint or delete it first")
				return
			}
		}
	}
	if err := s.deps.Q.DeleteComputeProvider(ctx, row.ID); err != nil {
		apiInternal(ctx, c, "failed to delete compute provider")
		return
	}
	s.recordAudit(ctx, "compute_provider.delete", "compute_provider")
	c.JSON(consts.StatusOK, utils.H{"success": true})
}

// handleTestComputeProvider dry-runs a Quote against the stored provider so an
// operator can verify configuration/credentials before pointing a pool at it.
func (s *Server) handleTestComputeProvider(ctx context.Context, c *app.RequestContext) {
	name := c.Param("name")
	row, err := s.deps.Q.GetComputeProvider(ctx, name)
	if err != nil {
		apiNotFound(ctx, c, "compute provider not found")
		return
	}
	creds, err := s.decryptProviderCredentials(row.CredentialsEnc)
	if err != nil {
		apiInternal(ctx, c, "failed to decrypt provider credentials")
		return
	}
	provider, err := compute.New(ctx, row.ProviderType, row.Name, row.Config, creds)
	if err != nil {
		apiBadRequest(ctx, c, "provider construction failed: "+err.Error())
		return
	}
	offers, err := provider.Quote(ctx, compute.Requirements{
		CPUMillis: 2000, MemoryMB: 4096, Arch: "amd64", Capacity: compute.CapacityAny,
	})
	if err != nil {
		c.JSON(consts.StatusOK, utils.H{"ok": false, "error": err.Error()})
		return
	}
	c.JSON(consts.StatusOK, utils.H{"ok": true, "offers": offers, "elastic": len(offers) > 0})
}

func (s *Server) upsertComputeProvider(ctx context.Context, req computeProviderReq) (string, error) {
	cfgJSON, err := json.Marshal(req.Config)
	if err != nil {
		return "", err
	}
	if req.Config == nil {
		cfgJSON = []byte("{}")
	}
	params := db.UpsertComputeProviderParams{
		Name: req.Name, ProviderType: req.Type, Config: cfgJSON,
	}
	// COALESCE in the query keeps existing credentials when none are sent, so
	// config edits never require re-entering secrets.
	if len(req.Credentials) > 0 {
		credsJSON, err := json.Marshal(req.Credentials)
		if err != nil {
			return "", err
		}
		enc, err := s.encryptWithMasterKey(credsJSON)
		if err != nil {
			return "", err
		}
		params.CredentialsEnc = enc
	}
	return s.deps.Q.UpsertComputeProvider(ctx, params)
}

// decryptProviderCredentials returns nil for providers using ambient auth.
func (s *Server) decryptProviderCredentials(enc []byte) ([]byte, error) {
	if len(enc) == 0 {
		return nil, nil
	}
	masterKey, err := hex.DecodeString(s.deps.Config.Encryption.MasterKey)
	if err != nil || len(masterKey) != 32 {
		return nil, errors.New("server encryption master key is not configured")
	}
	plain, _, err := secret.Decrypt(enc, masterKey)
	return plain, err
}

func isRegisteredComputeType(t string) bool {
	return slices.Contains(compute.Types(), t)
}
