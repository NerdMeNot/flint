package server

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/pkg/secret"
)

// forgeConnectionRequest is the create/update payload for a forge connection.
// Credentials are accepted here, envelope-encrypted, and stored in the DB —
// forge connections are DB-managed (previously a CRD reconciled from K8s Secrets).
type forgeConnectionRequest struct {
	Type        string `json:"type"` // github | gitlab | bitbucket
	DisplayName string `json:"displayName"`

	GitHub *struct {
		AppID          string `json:"appId"`
		InstallationID string `json:"installationId"`
		PrivateKey     string `json:"privateKey"`    // PEM
		WebhookSecret  string `json:"webhookSecret"` // stored plaintext (used for signature verification)
	} `json:"github,omitempty"`

	GitLab *struct {
		BaseURL      string `json:"baseUrl,omitempty"`
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	} `json:"gitlab,omitempty"`

	Bitbucket *struct {
		ClientID     string `json:"clientId"`
		ClientSecret string `json:"clientSecret"`
	} `json:"bitbucket,omitempty"`
}

// handleCreateForgeConnection creates a new forge connection.
func (s *Server) handleCreateForgeConnection(ctx context.Context, c *app.RequestContext) {
	var req forgeConnectionRequest
	if err := c.BindJSON(&req); err != nil || req.Type == "" {
		apiBadRequest(ctx, c, "type is required")
		return
	}
	if req.DisplayName == "" {
		req.DisplayName = req.Type
	}

	credsEnc, webhookSecret, err := s.encryptForgeCredentials(&req)
	if err != nil {
		apiBadRequest(ctx, c, err.Error())
		return
	}

	id, err := s.deps.Q.InsertForgeConnection(ctx, db.InsertForgeConnectionParams{
		ForgeType:      req.Type,
		DisplayName:    req.DisplayName,
		WebhookSecret:  webhookSecret,
		CredentialsEnc: credsEnc,
	})
	if err != nil {
		apiConflict(ctx, c, "could not create forge connection (it may already exist)")
		return
	}

	s.recordAudit(ctx, "connection.created", "forge_connection")
	c.JSON(consts.StatusCreated, utils.H{"id": id, "type": req.Type, "displayName": req.DisplayName})
}

// handleUpdateForgeConnection updates an existing forge connection (keyed by display name).
func (s *Server) handleUpdateForgeConnection(ctx context.Context, c *app.RequestContext) {
	var req forgeConnectionRequest
	if err := c.BindJSON(&req); err != nil || req.Type == "" || req.DisplayName == "" {
		apiBadRequest(ctx, c, "type and displayName are required")
		return
	}

	credsEnc, webhookSecret, err := s.encryptForgeCredentials(&req)
	if err != nil {
		apiBadRequest(ctx, c, err.Error())
		return
	}

	id, err := s.deps.Q.UpdateForgeConnectionByName(ctx, db.UpdateForgeConnectionByNameParams{
		ForgeType:      req.Type,
		DisplayName:    req.DisplayName,
		WebhookSecret:  webhookSecret,
		CredentialsEnc: credsEnc,
		DisplayName_2:  req.DisplayName,
	})
	if err != nil {
		apiNotFound(ctx, c, "forge connection not found")
		return
	}

	s.recordAudit(ctx, "connection.updated", "forge_connection")
	c.JSON(consts.StatusOK, utils.H{"id": id, "type": req.Type, "displayName": req.DisplayName})
}

// handleDeleteForgeConnection deletes a forge connection by ID.
func (s *Server) handleDeleteForgeConnection(ctx context.Context, c *app.RequestContext) {
	id := c.Param("id")
	if id == "" {
		apiBadRequest(ctx, c, "id is required")
		return
	}
	if err := s.deps.Q.DeleteForgeConnectionByID(ctx, id); err != nil {
		apiInternal(ctx, c, "failed to delete forge connection")
		return
	}
	s.recordAudit(ctx, "connection.deleted", "forge_connection")
	c.JSON(consts.StatusOK, utils.H{"status": "deleted", "id": id})
}

// encryptForgeCredentials builds the per-forge credential blob and envelope-encrypts
// it with the server master key. Returns the encrypted blob and (for GitHub) the
// webhook secret, which is stored in plaintext for signature verification.
func (s *Server) encryptForgeCredentials(req *forgeConnectionRequest) (credsEnc []byte, webhookSecret string, err error) {
	var creds map[string]string
	switch req.Type {
	case "github":
		if req.GitHub == nil {
			return nil, "", errors.New("github config is required when type is github")
		}
		creds = map[string]string{
			"appId":          req.GitHub.AppID,
			"installationId": req.GitHub.InstallationID,
			"privateKey":     req.GitHub.PrivateKey,
		}
		webhookSecret = req.GitHub.WebhookSecret
	case "gitlab":
		if req.GitLab == nil {
			return nil, "", errors.New("gitlab config is required when type is gitlab")
		}
		creds = map[string]string{
			"baseUrl":      req.GitLab.BaseURL,
			"clientId":     req.GitLab.ClientID,
			"clientSecret": req.GitLab.ClientSecret,
		}
	case "bitbucket":
		if req.Bitbucket == nil {
			return nil, "", errors.New("bitbucket config is required when type is bitbucket")
		}
		creds = map[string]string{
			"clientId":     req.Bitbucket.ClientID,
			"clientSecret": req.Bitbucket.ClientSecret,
		}
	default:
		return nil, "", fmt.Errorf("unsupported forge type %q", req.Type)
	}

	credsJSON, err := json.Marshal(creds)
	if err != nil {
		return nil, "", fmt.Errorf("serializing credentials: %w", err)
	}

	credsEnc, err = s.encryptWithMasterKey(credsJSON)
	if err != nil {
		return nil, "", err
	}
	return credsEnc, webhookSecret, nil
}

// encryptWithMasterKey envelope-encrypts plaintext with the server's configured
// master key. Used for any credential/secret blob stored at rest in the DB.
func (s *Server) encryptWithMasterKey(plaintext []byte) ([]byte, error) {
	masterKey, err := hex.DecodeString(s.deps.Config.Encryption.MasterKey)
	if err != nil || len(masterKey) != 32 {
		return nil, errors.New("server encryption master key is not configured")
	}
	version := byte(s.deps.Config.Encryption.MasterKeyVersion)
	if version == 0 {
		version = 1
	}
	return secret.Encrypt(plaintext, masterKey, version)
}

// recordAudit writes a best-effort audit entry for an administrative mutation.
func (s *Server) recordAudit(ctx context.Context, action, resourceType string) {
	claims := claimsFromCtx(ctx)
	if claims == nil {
		return
	}
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		return
	}
	user, err := s.deps.Q.GetUserByEmail(ctx, db.GetUserByEmailParams{OrgID: org.ID, Email: claims.Email})
	if err != nil {
		return
	}
	_ = s.deps.Q.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		OrgID:        org.ID,
		UserID:       &user.ID,
		Action:       action,
		ResourceType: resourceType,
	})
}
