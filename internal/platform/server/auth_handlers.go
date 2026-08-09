package server

import (
	"context"
	"encoding/json"
	"net"
	"net/netip"
	"strings"
	"time"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/common/utils"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/observe"
)

// recordLoginMetric counts a sign-in outcome by method (local/oidc/saml) and
// result (success/failure) for dashboards and failure-spike alerting.
func recordLoginMetric(ctx context.Context, method, result string) {
	observe.AuthLoginsTotal.Add(ctx, 1, metric.WithAttributes(
		attribute.String("method", method), attribute.String("result", result)))
}

// loginMethodFromReason derives the sign-in method from an auditLoginFailure
// reason string (e.g. "oidc:exchange_failed" -> "oidc", "require_sso" -> "local").
func loginMethodFromReason(reason string) string {
	switch {
	case strings.HasPrefix(reason, "oidc"):
		return "oidc"
	case strings.HasPrefix(reason, "saml"):
		return "saml"
	default:
		return "local"
	}
}

// Device authorization and MFA-pending state are stored in Postgres (see
// device_codes / mfa_pending_tokens) rather than process memory, so the auth
// layer is correct under concurrency, survives restarts, and works across
// replicas. See the auth_stores migration.

// registerAuthRoutes registers all auth endpoints.
func (s *Server) registerAuthRoutes() {
	s.hertz.POST("/auth/device/code", s.handleDeviceCode)
	s.hertz.POST("/auth/device/token", s.handleDeviceToken)
	s.hertz.POST("/auth/refresh", s.handleRefresh)
	s.hertz.POST("/auth/logout", s.handleLogout)
	s.hertz.GET("/auth/me", s.authMiddleware(), s.handleAuthMe)
	s.hertz.PUT("/auth/profile", s.authMiddleware(), s.handleUpdateProfile)
	s.hertz.GET("/auth/sessions", s.authMiddleware(), s.handleListSessions)
	s.hertz.DELETE("/auth/sessions/:id", s.authMiddleware(), s.handleRevokeSession)

	// Local auth — no JWT required.
	s.hertz.POST("/auth/login", s.handlePasswordLogin)
	s.hertz.POST("/auth/mfa/verify", s.handleMFAVerify)

	// Password + MFA management — JWT required.
	s.hertz.POST("/auth/change-password", s.authMiddleware(), s.handleChangePassword)
	s.hertz.POST("/auth/mfa/setup", s.authMiddleware(), s.handleMFASetup)
	s.hertz.POST("/auth/mfa/setup/verify", s.authMiddleware(), s.handleMFASetupVerify)
	s.hertz.POST("/auth/mfa/recovery-codes", s.authMiddleware(), s.handleRegenerateRecoveryCodes)
	s.hertz.DELETE("/auth/mfa", s.authMiddleware(), s.handleMFADisable)

	// SSO routes — no JWT required (these establish the JWT). Registered
	// unconditionally so that providers configured via the API (hot-reloaded
	// after boot) have working callbacks without a restart; the handlers guard
	// on the provider being configured at request time. Per-IP rate limited to
	// blunt brute-force/DoS against the unauthenticated auth surface.
	authLimit := s.ipRateLimit(newIPRateLimiter(5, 10))
	s.hertz.GET("/auth/login", authLimit, s.handleLogin)
	s.hertz.GET("/auth/oidc/callback", authLimit, s.handleOIDCCallback)
	s.hertz.POST("/auth/saml/acs", authLimit, s.handleSAMLACS)
	s.hertz.GET("/auth/saml/metadata", s.handleSAMLMetadata)

	// Single Logout endpoints: the post-logout redirect target (OIDC) and the
	// SAML SingleLogout service (SP-initiated response + IdP-initiated request).
	s.hertz.GET("/auth/oidc/logout-complete", s.handleOIDCLogoutComplete)
	s.hertz.POST("/auth/oidc/backchannel-logout", authLimit, s.handleOIDCBackchannelLogout)
	s.hertz.GET("/auth/saml/slo", authLimit, s.handleSAMLSLO)
	s.hertz.POST("/auth/saml/slo", authLimit, s.handleSAMLSLO)
}

// auditEvent writes a best-effort audit entry that is NOT tied to an
// authenticated user JWT — used for failed logins and SCIM provisioning.
// userID/resourceID may be empty (stored as NULL).
func (s *Server) auditEvent(ctx context.Context, orgID, userID, action, resourceType, resourceID string, ip netip.Addr) {
	var uid, rid *string
	if userID != "" {
		uid = &userID
	}
	if resourceID != "" {
		rid = &resourceID
	}
	_ = s.deps.Q.InsertAuditEntry(ctx, db.InsertAuditEntryParams{
		OrgID:        orgID,
		UserID:       uid,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   rid,
		Column6:      ip,
	})
}

// auditLoginFailure records a failed SSO login with a short reason code and the
// raw error detail (stored in metadata) so the sign-in log can show exactly which
// field/check failed.
func (s *Server) auditLoginFailure(ctx context.Context, c *app.RequestContext, reason, detail string) {
	recordLoginMetric(ctx, loginMethodFromReason(reason), "failure")
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		return
	}
	meta, _ := json.Marshal(map[string]string{"reason": reason, "detail": detail})
	rid := reason
	_ = s.deps.Q.InsertAuditEntryWithMeta(ctx, db.InsertAuditEntryWithMetaParams{
		OrgID:        org.ID,
		Action:       "auth.login.failed",
		ResourceType: "session",
		ResourceID:   &rid,
		Column6:      extractClientIP(c),
		Metadata:     meta,
	})
}

// handleSignInLog returns the recent SSO sign-in attempts (success + failure)
// with field-level diagnostics — the per-connection sign-in history.
func (s *Server) handleSignInLog(ctx context.Context, c *app.RequestContext) {
	org, err := s.deps.Q.GetOrg(ctx)
	if err != nil {
		apiInternal(ctx, c, "failed to load org")
		return
	}
	rows, err := s.deps.Q.ListRecentSignIns(ctx, db.ListRecentSignInsParams{OrgID: org.ID, Limit: 30})
	if err != nil {
		logErr(ctx, err, "list sign-ins")
		apiInternal(ctx, c, "failed to list sign-ins")
		return
	}
	events := make([]utils.H, 0, len(rows))
	for _, r := range rows {
		e := utils.H{
			"time":   r.CreatedAt.UTC().Format(time.RFC3339),
			"result": "success",
			"email":  derefStr(r.UserEmail),
			"ip":     r.IpAddress,
		}
		if r.Action == "auth.login.failed" {
			e["result"] = "failure"
			var m struct {
				Reason string `json:"reason"`
				Detail string `json:"detail"`
			}
			_ = json.Unmarshal(r.Metadata, &m)
			e["reason"] = m.Reason
			e["detail"] = m.Detail
			e["summary"] = classifyAuthError(m.Detail, m.Reason)
		}
		events = append(events, e)
	}
	c.JSON(consts.StatusOK, utils.H{"events": events})
}

// classifyAuthError turns a raw provider error into a short, human-readable,
// field-level explanation (the WorkOS/Scalekit diagnostics model).
func classifyAuthError(detail, reason string) string {
	d := strings.ToLower(detail)
	switch {
	case reason == "no_provider":
		return "No SSO provider configured"
	case strings.Contains(d, "audience"):
		return "Audience (SP Entity ID) mismatch"
	case strings.Contains(d, "nonce"):
		return "Nonce mismatch (possible replay)"
	case strings.Contains(d, "destination") || strings.Contains(d, "recipient"):
		return "ACS URL / destination mismatch"
	case strings.Contains(d, "signature") || strings.Contains(d, "verifying"):
		return "Signature verification failed"
	case strings.Contains(d, "expired") || strings.Contains(d, "notonorafter") || strings.Contains(d, "clock") || strings.Contains(d, "issue delay"):
		return "Assertion expired or clock skew"
	case strings.Contains(d, "email"):
		return "No email returned by the IdP"
	case strings.Contains(d, "issuer") || strings.Contains(d, "iss "):
		return "Issuer mismatch"
	default:
		return "Authentication failed"
	}
}

// derefStr returns the value of a *string, or "" if nil.
func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

const authSuccessHTML = `<!DOCTYPE html>
<html><head><title>Flint - Authentication Successful</title>
<style>body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#0f172a;color:#e2e8f0}
.card{text-align:center;padding:3rem;border-radius:12px;background:#1e293b;max-width:400px}
h1{color:#22c55e;font-size:1.5rem;margin-bottom:0.5rem}
p{color:#94a3b8;margin-top:0.5rem}</style></head>
<body><div class="card"><h1>Authentication Successful</h1>
<p>You can close this tab and return to the CLI.</p></div></body></html>`

func authErrorHTML(msg string) string {
	return `<!DOCTYPE html>
<html><head><title>Flint - Authentication Error</title>
<style>body{font-family:system-ui,sans-serif;display:flex;justify-content:center;align-items:center;min-height:100vh;margin:0;background:#0f172a;color:#e2e8f0}
.card{text-align:center;padding:3rem;border-radius:12px;background:#1e293b;max-width:400px}
h1{color:#ef4444;font-size:1.5rem;margin-bottom:0.5rem}
p{color:#94a3b8;margin-top:0.5rem}</style></head>
<body><div class="card"><h1>Authentication Error</h1>
<p>` + msg + `</p></div></body></html>`
}

// extractClientIP extracts and parses the client IP from the request.
func extractClientIP(c *app.RequestContext) netip.Addr {
	ipStr := string(c.GetHeader("X-Real-IP"))
	if ipStr == "" {
		ipStr = string(c.GetHeader("X-Forwarded-For"))
		if idx := strings.Index(ipStr, ","); idx > 0 {
			ipStr = strings.TrimSpace(ipStr[:idx])
		}
	}
	if ipStr == "" {
		ipStr = c.RemoteAddr().String()
	}

	// Try parsing directly first.
	if addr, err := netip.ParseAddr(ipStr); err == nil {
		return addr
	}

	// Strip port if present (e.g., "1.2.3.4:8080" or "[::1]:8080").
	if host, _, err := net.SplitHostPort(ipStr); err == nil {
		if addr, err := netip.ParseAddr(host); err == nil {
			return addr
		}
	}

	return netip.Addr{} // zero value — rate limiting still works, just less precise
}

// StartAuthStoreCleanup runs a background goroutine that periodically prunes
// expired device codes and MFA-pending tokens from Postgres. Call once at
// startup with a process-lifetime context; the goroutine exits on ctx.Done.
// (The old in-memory cleanup function was defined but never invoked — a leak.)
func (s *Server) StartAuthStoreCleanup(ctx context.Context) {
	if s.deps.Q == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = s.deps.Q.DeleteExpiredDeviceCodes(ctx)
				_ = s.deps.Q.DeleteExpiredMFAPendingTokens(ctx)
			}
		}
	}()
}

// logErr logs an error with context.
func logErr(ctx context.Context, err error, msg string) {
	logger := observe.Logger(ctx)
	logger.Error().Err(err).Msg(msg)
}

// Ensure observe is used.
var _ = observe.RequestID
