package server

import (
	"context"
	"encoding/json"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// handleListAuthProviders returns configured SSO providers (without secrets).
func (s *Server) handleListAuthProviders(ctx context.Context, c *app.RequestContext) {
	providers, err := s.deps.Q.ListAuthProviderConfigNames(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to list auth providers")
		return
	}

	var result []utils.H
	for _, p := range providers {
		result = append(result, utils.H{
			"id":           p.ID,
			"providerType": p.ProviderType,
			"displayName":  p.DisplayName,
		})
	}

	// Include current OIDC config status (from config file).
	oidcConfigured := s.deps.OIDCProvider != nil
	samlConfigured := s.deps.SAMLProvider != nil

	c.JSON(consts.StatusOK, utils.H{
		"providers":      result,
		"oidcConfigured": oidcConfigured,
		"samlConfigured": samlConfigured,
	})
}

// handleUpdateAuthProvider configures or updates an SSO provider.
// This stores the config in the auth_provider_config table and can
// trigger a hot-reload of the OIDC/SAML provider on the server.
func (s *Server) handleUpdateAuthProvider(ctx context.Context, c *app.RequestContext) {
	var req struct {
		ProviderType string              `json:"providerType"` // "oidc" or "saml"
		DisplayName  string              `json:"displayName"`
		Config       auth.ProviderConfig `json:"config"`
	}
	if err := c.BindJSON(&req); err != nil || req.ProviderType == "" {
		apiBadRequest(ctx, c, "providerType is required")
		return
	}

	if req.ProviderType != "oidc" && req.ProviderType != "saml" {
		apiBadRequest(ctx, c, "providerType must be 'oidc' or 'saml'")
		return
	}

	if req.DisplayName == "" {
		req.DisplayName = req.ProviderType
	}

	// Build (and validate) the provider before persisting, so a bad config is
	// rejected rather than saved. The freshly-built provider is hot-swapped into
	// Deps so the change takes effect without a restart.
	baseURL := s.deps.Config.Server.BaseURL
	switch req.ProviderType {
	case "oidc":
		if !req.Config.HasOIDC() {
			apiBadRequest(ctx, c, "OIDC requires issuerUrl and clientId")
			return
		}
		provider, buildErr := auth.BuildOIDCProvider(ctx, req.Config, baseURL)
		if buildErr != nil {
			apiBadRequest(ctx, c, "OIDC provider could not be initialized: "+buildErr.Error())
			return
		}
		s.deps.OIDCProvider = provider
	case "saml":
		if !req.Config.HasSAML() {
			apiBadRequest(ctx, c, "SAML requires metadataUrl or metadataXml")
			return
		}
		provider, buildErr := auth.BuildSAMLProvider(req.Config, baseURL)
		if buildErr != nil {
			apiBadRequest(ctx, c, "SAML provider could not be initialized: "+buildErr.Error())
			return
		}
		s.deps.SAMLProvider = provider
	}

	// Serialize then envelope-encrypt the config (it contains the client secret).
	configJSON, err := json.Marshal(req.Config)
	if err != nil {
		apiInternal(ctx, c, "failed to serialize config")
		return
	}
	configEnc, err := s.encryptWithMasterKey(configJSON)
	if err != nil {
		apiInternal(ctx, c, "failed to encrypt config")
		return
	}

	_, err = s.deps.Q.UpsertAuthProviderConfig(ctx, db.UpsertAuthProviderConfigParams{
		ProviderType: req.ProviderType,
		DisplayName:  req.DisplayName,
		ConfigEnc:    configEnc,
	})
	if err != nil {
		apiInternal(ctx, c, "failed to save auth provider config")
		return
	}

	// Audit.
	claims := claimsFromCtx(ctx)
	if claims != nil {
		org, _ := s.deps.Q.GetOrg(ctx)
		user, _ := s.deps.Q.GetUserByEmail(ctx, db.GetUserByEmailParams{
			OrgID: org.ID, Email: claims.Email,
		})
		_ = s.deps.Q.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
			OrgID: org.ID, UserID: &user.ID,
			Action: "auth.provider.configured", ResourceType: "auth_provider",
		})
	}

	c.JSON(consts.StatusOK, utils.H{"status": "configured", "providerType": req.ProviderType})
}

// handleTestAuthProvider builds a provider from the supplied config WITHOUT
// persisting it — running OIDC discovery / fetching SAML metadata — so the admin
// can validate a connection before activating it. It always responds 200; the
// body's "ok" field reports success so the UI can render inline feedback.
func (s *Server) handleTestAuthProvider(ctx context.Context, c *app.RequestContext) {
	var req struct {
		ProviderType string              `json:"providerType"`
		Config       auth.ProviderConfig `json:"config"`
	}
	if err := c.BindJSON(&req); err != nil {
		apiBadRequest(ctx, c, "invalid request body")
		return
	}

	baseURL := s.deps.Config.Server.BaseURL
	switch req.ProviderType {
	case "oidc":
		if !req.Config.HasOIDC() {
			c.JSON(consts.StatusOK, utils.H{"ok": false, "error": "issuerUrl and clientId are required"})
			return
		}
		provider, err := auth.BuildOIDCProvider(ctx, req.Config, baseURL)
		if err != nil {
			c.JSON(consts.StatusOK, utils.H{"ok": false, "error": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, utils.H{"ok": true, "oidc": provider.Discovery()})
	case "saml":
		if !req.Config.HasSAML() {
			c.JSON(consts.StatusOK, utils.H{"ok": false, "error": "metadataUrl or metadataXml is required"})
			return
		}
		provider, err := auth.BuildSAMLProvider(req.Config, baseURL)
		if err != nil {
			c.JSON(consts.StatusOK, utils.H{"ok": false, "error": err.Error()})
			return
		}
		c.JSON(consts.StatusOK, utils.H{"ok": true, "saml": provider.Discovery()})
	default:
		apiBadRequest(ctx, c, "providerType must be 'oidc' or 'saml'")
	}
}

// handleGetGroupMappings returns the org's IdP group→role mappings and the
// strict-groups (deny-by-default) flag.
func (s *Server) handleGetGroupMappings(ctx context.Context, c *app.RequestContext) {
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to load org")
		return
	}
	rows, err := s.deps.Q.ListSSOGroupRoleMappings(ctx, org.ID)
	if err != nil {
		apiInternal(ctx, c, "failed to list group mappings")
		return
	}
	mappings := make([]utils.H, 0, len(rows))
	for _, m := range rows {
		mappings = append(mappings, utils.H{
			"groupName": m.GroupName,
			"roleId":    m.RoleID,
			"roleSlug":  m.RoleSlug,
			"roleName":  m.RoleName,
		})
	}
	c.JSON(consts.StatusOK, utils.H{"mappings": mappings, "strict": org.SsoStrictGroups})
}

// handlePutGroupMappings replaces the full set of group→role mappings and sets
// the strict-groups flag. Changes take effect on each user's next login/sync.
func (s *Server) handlePutGroupMappings(ctx context.Context, c *app.RequestContext) {
	var req struct {
		Strict   bool `json:"strict"`
		Mappings []struct {
			GroupName string `json:"groupName"`
			RoleID    string `json:"roleId"`
		} `json:"mappings"`
	}
	if err := c.BindJSON(&req); err != nil {
		apiBadRequest(ctx, c, "invalid request body")
		return
	}

	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to load org")
		return
	}

	tx, err := s.deps.DB.Begin(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to begin transaction")
		return
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op after commit
	qtx := db.New(tx)

	if err := qtx.DeleteAllSSOGroupRoleMappings(ctx, org.ID); err != nil {
		apiInternal(ctx, c, "failed to clear group mappings")
		return
	}
	for _, m := range req.Mappings {
		if m.GroupName == "" || m.RoleID == "" {
			continue
		}
		if err := qtx.InsertSSOGroupRoleMapping(ctx, db.InsertSSOGroupRoleMappingParams{
			OrgID:     org.ID,
			GroupName: m.GroupName,
			RoleID:    m.RoleID,
		}); err != nil {
			apiBadRequest(ctx, c, "failed to save mapping (unknown role?): "+err.Error())
			return
		}
	}
	if err := qtx.SetOrgStrictGroups(ctx, db.SetOrgStrictGroupsParams{Strict: req.Strict, ID: org.ID}); err != nil {
		apiInternal(ctx, c, "failed to set strict flag")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		apiInternal(ctx, c, "failed to commit")
		return
	}

	s.recordAudit(ctx, "auth.group_mappings.updated", "auth_provider")
	c.JSON(consts.StatusOK, utils.H{"status": "saved"})
}

// handleDeleteAuthProvider removes an SSO provider configuration.
func (s *Server) handleDeleteAuthProvider(ctx context.Context, c *app.RequestContext) {
	providerType := c.Param("type")
	if providerType != "oidc" && providerType != "saml" {
		apiBadRequest(ctx, c, "type must be 'oidc' or 'saml'")
		return
	}

	n, err := s.deps.Q.DeleteAuthProviderConfig(ctx, providerType)
	if err != nil {
		apiInternal(ctx, c, "failed to delete auth provider")
		return
	}
	if n == 0 {
		apiNotFound(ctx, c, "auth provider not configured")
		return
	}

	// Drop the hot-loaded provider so it stops being offered immediately.
	if providerType == "oidc" {
		s.deps.OIDCProvider = nil
	} else {
		s.deps.SAMLProvider = nil
	}

	s.recordAudit(ctx, "auth.provider.deleted", "auth_provider")
	c.JSON(consts.StatusOK, utils.H{"status": "deleted", "providerType": providerType})
}
