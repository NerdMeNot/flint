package server

import (
	"context"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/platform/auth"
)

// handleMFAVerify completes the second phase of login with a TOTP code.
func (s *Server) handleMFAVerify(ctx context.Context, c *app.RequestContext) {
	var req struct {
		MFAToken     string `json:"mfaToken"`
		Code         string `json:"code"`
		RecoveryCode string `json:"recoveryCode"`
	}
	if err := c.BindJSON(&req); err != nil || req.MFAToken == "" {
		apiBadRequest(ctx, c, "mfaToken is required")
		return
	}
	if req.Code == "" && req.RecoveryCode == "" {
		apiBadRequest(ctx, c, "code or recoveryCode is required")
		return
	}

	// Look up pending MFA entry (the query filters out expired tokens).
	entry, err := s.deps.Q.GetMFAPendingToken(ctx, req.MFAToken)
	if err != nil {
		apiUnauthorized(ctx, c, "invalid or expired MFA token")
		return
	}

	// Get user's TOTP secret.
	user, err := s.deps.Q.GetUserForAuth(ctx, db.GetUserForAuthParams{
		OrgID: entry.OrgID, Email: entry.Email,
	})
	if err != nil || user.TotpSecretEnc == nil {
		apiUnauthorized(ctx, c, "MFA not configured")
		return
	}

	valid := false

	if req.Code != "" {
		// Validate TOTP code, then guard against replay (see checkTOTPReplay).
		valid = auth.ValidateTOTPCode(string(user.TotpSecretEnc), req.Code) &&
			s.checkTOTPReplay(ctx, user.ID)
	} else if req.RecoveryCode != "" {
		// Validate recovery code (single-use).
		codes, _ := s.deps.Q.GetUserRecoveryCodes(ctx, user.ID)
		var remaining []string
		remaining, valid = auth.ValidateRecoveryCode(req.RecoveryCode, codes)
		if valid {
			_ = s.deps.Q.SetUserRecoveryCodes(ctx, db.SetUserRecoveryCodesParams{
				ID:            user.ID,
				RecoveryCodes: remaining,
			})
		}
	}

	if !valid {
		apiUnauthorized(ctx, c, "invalid code")
		return
	}

	// Remove used MFA token.
	_ = s.deps.Q.DeleteMFAPendingToken(ctx, req.MFAToken)

	// Issue tokens.
	s.issueLocalAuthTokens(ctx, c, entry.UserID, entry.Email, entry.OrgID, false)
}

// checkTOTPReplay records the current TOTP period for the user and returns true
// only if it is strictly newer than the last accepted one — rejecting reuse of a
// code within (or before) its validity window. Replica-safe: the advance is a
// single atomic UPDATE.
func (s *Server) checkTOTPReplay(ctx context.Context, userID string) bool {
	period := auth.TOTPPeriod(time.Now())
	_, err := s.deps.Q.RecordTOTPUse(ctx, db.RecordTOTPUseParams{
		ID:                userID,
		MfaLastUsedPeriod: pgtype.Int8{Int64: period, Valid: true},
	})
	return err == nil
}

// handleMFASetup generates a new TOTP secret and returns the QR code URL.
func (s *Server) handleMFASetup(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	if claims == nil {
		apiUnauthorized(ctx, c, "not authenticated")
		return
	}

	issuer := "Flint"
	if s.deps.Config != nil && s.deps.Config.Server.BaseURL != "" {
		issuer = s.deps.Config.Server.BaseURL
	}

	key, err := auth.GenerateTOTPSecret(claims.Email, issuer)
	if err != nil {
		apiInternal(ctx, c, "failed to generate TOTP secret")
		return
	}

	// Store the secret (unverified — user must confirm with a code first).
	user, err := s.deps.Q.GetUserByEmail(ctx, db.GetUserByEmailParams{
		OrgID: claims.OrgID, Email: claims.Email,
	})
	if err != nil {
		apiNotFound(ctx, c, "user not found")
		return
	}

	_ = s.deps.Q.SetUserTOTPSecret(ctx, db.SetUserTOTPSecretParams{
		ID:            user.ID,
		TotpSecretEnc: []byte(key.Secret()),
	})

	// Generate recovery codes.
	rawCodes, hashedCodes, err := auth.GenerateRecoveryCodes(8)
	if err != nil {
		apiInternal(ctx, c, "failed to generate recovery codes")
		return
	}

	// Store hashed recovery codes.
	_ = s.deps.Q.SetUserRecoveryCodes(ctx, db.SetUserRecoveryCodesParams{
		ID:            user.ID,
		RecoveryCodes: hashedCodes,
	})

	c.JSON(consts.StatusOK, utils.H{
		"secret":        key.Secret(),
		"qrCodeURL":     key.URL(),
		"recoveryCodes": rawCodes,
	})
}

// handleMFASetupVerify confirms the TOTP setup by validating a code.
func (s *Server) handleMFASetupVerify(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	if claims == nil {
		apiUnauthorized(ctx, c, "not authenticated")
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := c.BindJSON(&req); err != nil || req.Code == "" {
		apiBadRequest(ctx, c, "code is required")
		return
	}

	user, err := s.deps.Q.GetUserForAuth(ctx, db.GetUserForAuthParams{
		OrgID: claims.OrgID, Email: claims.Email,
	})
	if err != nil || user.TotpSecretEnc == nil {
		apiNotFound(ctx, c, "TOTP not set up — call /auth/mfa/setup first")
		return
	}

	if !auth.ValidateTOTPCode(string(user.TotpSecretEnc), req.Code) || !s.checkTOTPReplay(ctx, user.ID) {
		apiBadRequest(ctx, c, "invalid code — scan the QR code and try again")
		return
	}

	_ = s.deps.Q.VerifyUserTOTP(ctx, user.ID)

	// Recovery codes were already generated, stored, and shown to the user at
	// /auth/mfa/setup; do not regenerate here (that would invalidate the codes
	// they just saved). Rotation is available via /auth/mfa/recovery-codes.

	// Audit.
	org, _ := s.deps.Q.GetOrg(ctx)
	_ = s.deps.Q.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		OrgID: org.ID, UserID: &user.ID,
		Action: "auth.mfa.enabled", ResourceType: "user",
	})

	c.JSON(consts.StatusOK, utils.H{"status": "mfa_enabled"})
}

// handleRegenerateRecoveryCodes issues a fresh set of one-time recovery codes,
// invalidating the previous set. Requires a valid current TOTP or recovery code
// so a hijacked session can't silently rotate the user's break-glass codes.
func (s *Server) handleRegenerateRecoveryCodes(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	if claims == nil {
		apiUnauthorized(ctx, c, "not authenticated")
		return
	}
	var req struct {
		Code         string `json:"code"`
		RecoveryCode string `json:"recoveryCode"`
	}
	if err := c.BindJSON(&req); err != nil || (req.Code == "" && req.RecoveryCode == "") {
		apiBadRequest(ctx, c, "code or recoveryCode is required")
		return
	}

	user, err := s.deps.Q.GetUserForAuth(ctx, db.GetUserForAuthParams{
		OrgID: claims.OrgID, Email: claims.Email,
	})
	if err != nil || user.TotpSecretEnc == nil {
		apiNotFound(ctx, c, "MFA is not configured")
		return
	}

	// Re-authenticate the second factor before rotating the codes.
	valid := false
	if req.Code != "" {
		valid = auth.ValidateTOTPCode(string(user.TotpSecretEnc), req.Code) && s.checkTOTPReplay(ctx, user.ID)
	} else {
		codes, _ := s.deps.Q.GetUserRecoveryCodes(ctx, user.ID)
		if _, ok := auth.ValidateRecoveryCode(req.RecoveryCode, codes); ok {
			valid = true
		}
	}
	if !valid {
		apiUnauthorized(ctx, c, "invalid code")
		return
	}

	raw, hashed, err := auth.GenerateRecoveryCodes(10)
	if err != nil {
		apiInternal(ctx, c, "failed to generate recovery codes")
		return
	}
	if err := s.deps.Q.SetUserRecoveryCodes(ctx, db.SetUserRecoveryCodesParams{
		ID: user.ID, RecoveryCodes: hashed,
	}); err != nil {
		apiInternal(ctx, c, "failed to store recovery codes")
		return
	}

	org, _ := s.deps.Q.GetOrg(ctx)
	_ = s.deps.Q.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		OrgID: org.ID, UserID: &user.ID,
		Action: "auth.mfa.recovery_codes.regenerated", ResourceType: "user",
	})
	c.JSON(consts.StatusOK, utils.H{"recoveryCodes": raw})
}

// handleMFADisable disables MFA for the current user.
func (s *Server) handleMFADisable(ctx context.Context, c *app.RequestContext) {
	claims := claimsFromCtx(ctx)
	if claims == nil {
		apiUnauthorized(ctx, c, "not authenticated")
		return
	}

	var req struct {
		Code         string `json:"code"`
		RecoveryCode string `json:"recoveryCode"`
	}
	if err := c.BindJSON(&req); err != nil || (req.Code == "" && req.RecoveryCode == "") {
		apiBadRequest(ctx, c, "code or recoveryCode is required to disable MFA")
		return
	}

	user, err := s.deps.Q.GetUserForAuth(ctx, db.GetUserForAuthParams{
		OrgID: claims.OrgID, Email: claims.Email,
	})
	if err != nil || user.TotpSecretEnc == nil {
		apiNotFound(ctx, c, "MFA not enabled")
		return
	}

	// Verify current code before disabling.
	valid := false
	if req.Code != "" {
		valid = auth.ValidateTOTPCode(string(user.TotpSecretEnc), req.Code) && s.checkTOTPReplay(ctx, user.ID)
	} else if req.RecoveryCode != "" {
		codes, _ := s.deps.Q.GetUserRecoveryCodes(ctx, user.ID)
		_, valid = auth.ValidateRecoveryCode(req.RecoveryCode, codes)
	}

	if !valid {
		apiUnauthorized(ctx, c, "invalid code")
		return
	}

	_ = s.deps.Q.ClearUserTOTP(ctx, user.ID)

	// Audit.
	org, _ := s.deps.Q.GetOrg(ctx)
	_ = s.deps.Q.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		OrgID: org.ID, UserID: &user.ID,
		Action: "auth.mfa.disabled", ResourceType: "user",
	})

	c.JSON(consts.StatusOK, utils.H{"status": "mfa_disabled"})
}
