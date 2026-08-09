package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/platform/auth"
)

const deviceCodeTTL = 15 * time.Minute

// handleDeviceCode initiates the device authorization flow for TUI/CLI.
// Returns a device code and user code that the user enters in a browser.
func (s *Server) handleDeviceCode(ctx context.Context, c *app.RequestContext) {
	deviceCode := generateSecureCode(32)
	userCode := generateUserCode()

	if err := s.deps.Q.InsertDeviceCode(ctx, db.InsertDeviceCodeParams{
		DeviceCode:   deviceCode,
		UserCode:     userCode,
		ExpiresAt:    time.Now().Add(deviceCodeTTL),
		IntervalSecs: 5,
	}); err != nil {
		apiInternal(ctx, c, "failed to create device code")
		return
	}

	baseURL := s.deps.Config.Server.BaseURL
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	c.JSON(consts.StatusOK, utils.H{
		"deviceCode":      deviceCode,
		"userCode":        userCode,
		"verificationUri": fmt.Sprintf("%s/auth/device", baseURL),
		"expiresIn":       900,
		"interval":        5,
	})
}

// handleDeviceToken polls for device flow completion.
// Returns AUTHORIZATION_PENDING until the user completes auth.
func (s *Server) handleDeviceToken(ctx context.Context, c *app.RequestContext) {
	var req struct {
		DeviceCode string `json:"deviceCode"`
	}
	if err := c.BindJSON(&req); err != nil || req.DeviceCode == "" {
		apiBadRequest(ctx, c, "deviceCode is required")
		return
	}

	row, err := s.deps.Q.GetDeviceCode(ctx, req.DeviceCode)
	if err != nil {
		apiBadRequest(ctx, c, "invalid device code")
		return
	}

	if time.Now().After(row.ExpiresAt) {
		_ = s.deps.Q.DeleteDeviceCode(ctx, req.DeviceCode)
		apiError(ctx, c, consts.StatusBadRequest, "EXPIRED_TOKEN", "device code expired")
		return
	}

	// Enforce the polling interval (OAuth slow_down). Measured from the last
	// accepted poll; too-fast polls are rejected without advancing the marker.
	if row.LastPolledAt != nil &&
		time.Since(*row.LastPolledAt) < time.Duration(row.IntervalSecs)*time.Second {
		apiError(ctx, c, consts.StatusBadRequest, "SLOW_DOWN", "polling too frequently")
		return
	}
	_ = s.deps.Q.TouchDeviceCodePoll(ctx, req.DeviceCode)

	if !row.Completed {
		apiError(ctx, c, consts.StatusBadRequest, "AUTHORIZATION_PENDING", "waiting for user authorization")
		return
	}

	// Auth complete — atomically claim (return + delete) the tokens. The atomic
	// DELETE ... RETURNING closes the TOCTOU window the in-memory map had.
	tok, err := s.deps.Q.ClaimCompletedDeviceCode(ctx, req.DeviceCode)
	if err != nil {
		// Lost a race to another poll, or already claimed.
		apiError(ctx, c, consts.StatusBadRequest, "AUTHORIZATION_PENDING", "waiting for user authorization")
		return
	}

	c.JSON(consts.StatusOK, utils.H{
		"accessToken":  derefStr(tok.AccessToken),
		"refreshToken": derefStr(tok.RefreshToken),
		"expiresIn":    900, // 15 minutes
	})
}

// CompleteDeviceAuth is called when a user completes device flow auth (from browser).
// In production, this would be called after OIDC callback validates the user.
func (s *Server) CompleteDeviceAuth(ctx context.Context, deviceCode string, claims *auth.Claims, userID string) error {
	if s.deps.Sessions == nil {
		return fmt.Errorf("sessions not configured")
	}

	accessToken, err := s.deps.Sessions.CreateSession(claims)
	if err != nil {
		return err
	}

	// Create server-side session with refresh token.
	refreshRaw, refreshHash, err := auth.GenerateRefreshToken()
	if err != nil {
		return err
	}

	_, err = s.deps.Q.CreateSession(ctx, db.CreateSessionParams{
		UserID:           userID,
		TokenHash:        refreshHash,
		AbsLifetimeSecs:  30 * 24 * 3600, // 30 days
		IdleLifetimeSecs: 7 * 24 * 3600,  // 7 days
	})
	if err != nil {
		return fmt.Errorf("create session: %w", err)
	}

	uid := userID
	if err := s.deps.Q.CompleteDeviceCode(ctx, db.CompleteDeviceCodeParams{
		DeviceCode:   deviceCode,
		AccessToken:  &accessToken,
		RefreshToken: &refreshRaw,
		UserID:       &uid,
	}); err != nil {
		return fmt.Errorf("complete device code: %w", err)
	}
	return nil
}

func generateSecureCode(bytes int) string {
	b := make([]byte, bytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func generateUserCode() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	code := hex.EncodeToString(b)
	return fmt.Sprintf("FLNT-%s", code[:8])
}
