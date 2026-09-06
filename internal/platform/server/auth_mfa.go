package server

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/NerdMeNot/flint/pkg/secret"
)

// maxMFAAttempts is how many wrong second-factor guesses a single pending token
// tolerates before it is destroyed and the user has to start from the password
// again — which is itself rate limited, per email and per IP.
const maxMFAAttempts = 5

// MFA pending-token purposes. See the mfa_pending_tokens table.
const (
	mfaPurposeVerify = "verify"
	mfaPurposeEnrol  = "enrol"
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

	// An enrolment token is not a login credential: it exists only so a user
	// whose role requires MFA can set it up. Redeeming one here would hand out a
	// session to someone who has not presented a second factor.
	if entry.Purpose == mfaPurposeEnrol {
		apiUnauthorized(ctx, c, "this token is for MFA enrolment, not sign-in")
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
		valid = s.validateTOTP(ctx, user.TotpSecretEnc, req.Code) && s.checkTOTPReplay(ctx, user.ID)
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
		// Count the guess and retire the token once guessing is evident. Without
		// this the endpoint was an unbounded oracle over a 10^6 code space, and
		// over 64-bit recovery codes on the same request shape.
		attempts, err := s.deps.Q.RecordMFAAttemptFailure(ctx, req.MFAToken)
		if err != nil || attempts >= maxMFAAttempts {
			_ = s.deps.Q.DeleteMFAPendingToken(ctx, req.MFAToken)
			s.auditLoginFailure(ctx, c, "mfa:attempts_exhausted", entry.Email)
			apiUnauthorized(ctx, c, "too many incorrect codes — sign in again")
			return
		}
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
	orgID, email, ok := s.mfaEnrolIdentity(ctx, c)
	if !ok {
		return
	}
	claims := &auth.Claims{OrgID: orgID, Email: email}

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

	// Envelope-encrypt before storing. The column is named totp_secret_enc and
	// every other _enc column in the codebase goes through secret.Encrypt; this
	// one stored the raw base32 secret, so read access to the database was a
	// permanent, undetectable MFA bypass for every user.
	enc, err := s.encryptTOTPSecret(key.Secret())
	if err != nil {
		apiError(ctx, c, consts.StatusServiceUnavailable, "ENCRYPTION_UNAVAILABLE",
			"MFA cannot be enabled: server encryption master key is not configured")
		return
	}
	if err := s.deps.Q.SetUserTOTPSecret(ctx, db.SetUserTOTPSecretParams{
		ID:            user.ID,
		TotpSecretEnc: enc,
	}); err != nil {
		apiInternal(ctx, c, "failed to store TOTP secret")
		return
	}

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
	orgID, email, ok := s.mfaEnrolIdentity(ctx, c)
	if !ok {
		return
	}
	claims := &auth.Claims{OrgID: orgID, Email: email}

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

	if !s.validateTOTP(ctx, user.TotpSecretEnc, req.Code) || !s.checkTOTPReplay(ctx, user.ID) {
		apiBadRequest(ctx, c, "invalid code — scan the QR code and try again")
		return
	}

	_ = s.deps.Q.VerifyUserTOTP(ctx, user.ID)

	// Enrolment is done, so the token that authorised it is spent. Leaving it
	// live would keep a password-only credential valid for the rest of its
	// window after it has served its purpose.
	if tok := string(c.GetHeader("X-MFA-Enrolment-Token")); tok != "" {
		_ = s.deps.Q.DeleteMFAPendingToken(ctx, tok)
	}

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
		valid = s.validateTOTP(ctx, user.TotpSecretEnc, req.Code) && s.checkTOTPReplay(ctx, user.ID)
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
		valid = s.validateTOTP(ctx, user.TotpSecretEnc, req.Code) && s.checkTOTPReplay(ctx, user.ID)
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

// ── TOTP secret storage ──────────────────────────────────────
//
// The secret is envelope-encrypted with the server master key, like every other
// _enc column. Two consequences worth stating plainly:
//
//   - MFA cannot be enabled without a configured master key. Refusing is the
//     honest option; the alternative is storing the one credential that defends
//     a compromised password in a form anyone with a database handle can read.
//   - A secret that no longer decrypts (rotated or lost master key) makes TOTP
//     unusable for that user. That is deliberate rather than falling back to
//     treating the bytes as plaintext, which would keep the original hole open.
//     Recovery codes are hashed separately and still work, so there is a way
//     back in.

func (s *Server) encryptTOTPSecret(plain string) ([]byte, error) {
	if s.deps.Config == nil {
		return nil, fmt.Errorf("no config")
	}
	masterKey, err := s.deps.Config.Encryption.DecodeMasterKey()
	if err != nil {
		return nil, err
	}
	return secret.Encrypt([]byte(plain), masterKey, 1)
}

// validateTOTP decrypts a stored secret and checks the code against it.
func (s *Server) validateTOTP(ctx context.Context, enc []byte, code string) bool {
	if len(enc) == 0 || code == "" {
		return false
	}
	if s.deps.Config == nil {
		return false
	}
	masterKey, err := s.deps.Config.Encryption.DecodeMasterKey()
	if err != nil {
		logger := observe.Logger(ctx)
		logger.Error().Err(err).
			Msg("mfa: cannot verify TOTP, server encryption master key is not configured")
		return false
	}
	plain, _, err := secret.Decrypt(enc, masterKey)
	if err != nil {
		logger := observe.Logger(ctx)
		logger.Error().Err(err).
			Msg("mfa: stored TOTP secret could not be decrypted; the user must re-enrol via a recovery code")
		return false
	}
	return auth.ValidateTOTPCode(string(plain), code)
}

// mfaEnrolIdentity resolves who is enrolling a second factor, from either an
// authenticated session or an enrolment token issued at login.
//
// The token path exists because the two requirements were circular: a role
// requiring MFA refused to issue a session until the user enrolled, and
// enrolling required a session. The token is deliberately narrow — it is minted
// only after a correct password, expires in five minutes, cannot be redeemed
// for a session by /auth/mfa/verify, and is consumed once enrolment completes.
func (s *Server) mfaEnrolIdentity(ctx context.Context, c *app.RequestContext) (orgID, email string, ok bool) {
	if claims := claimsFromCtx(ctx); claims != nil {
		return claims.OrgID, claims.Email, true
	}

	token := string(c.GetHeader("X-MFA-Enrolment-Token"))
	if token == "" {
		apiUnauthorized(ctx, c, "not authenticated")
		return "", "", false
	}
	entry, err := s.deps.Q.GetMFAPendingToken(ctx, token)
	if err != nil || entry.Purpose != mfaPurposeEnrol {
		apiUnauthorized(ctx, c, "invalid or expired enrolment token")
		return "", "", false
	}
	return entry.OrgID, entry.Email, true
}
