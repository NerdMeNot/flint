package server

import (
	"context"
	"encoding/json"

	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/NerdMeNot/flint/internal/core/db"
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
		ProviderType string `json:"providerType"` // "oidc" or "saml"
		DisplayName  string `json:"displayName"`
		Config       struct {
			IssuerURL    string `json:"issuerUrl,omitempty"`
			ClientID     string `json:"clientId,omitempty"`
			ClientSecret string `json:"clientSecret,omitempty"`
			MetadataURL  string `json:"metadataUrl,omitempty"`
			EntityID     string `json:"entityId,omitempty"`
		} `json:"config"`
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

	// Try to hot-reload the OIDC provider if that's what was configured.
	if req.ProviderType == "oidc" && req.Config.IssuerURL != "" && req.Config.ClientID != "" {
		newProvider, oidcErr := auth.NewOIDCProvider(ctx, auth.OIDCProviderConfig{
			IssuerURL:    req.Config.IssuerURL,
			ClientID:     req.Config.ClientID,
			ClientSecret: req.Config.ClientSecret,
			RedirectURL:  s.deps.Config.Server.BaseURL + "/auth/oidc/callback",
		})
		if oidcErr == nil {
			s.deps.OIDCProvider = newProvider
		}
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
