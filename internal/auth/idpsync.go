package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/NerdMeNot/flint/internal/db"
	"github.com/NerdMeNot/flint/pkg/secret"
	"github.com/casbin/casbin/v2"
	"github.com/rs/zerolog/log"
	"golang.org/x/oauth2"
)

// IdPSyncConfig configures the IdP sync loop.
type IdPSyncConfig struct {
	Interval   time.Duration // default 15min
	BatchSize  int           // default 50
	StaleAfter time.Duration // re-sync users not checked in this window (default 1h)
}

func (c *IdPSyncConfig) interval() time.Duration {
	if c.Interval > 0 {
		return c.Interval
	}
	return 15 * time.Minute
}

func (c *IdPSyncConfig) batchSize() int32 {
	if c.BatchSize > 0 {
		return int32(c.BatchSize)
	}
	return 50
}

func (c *IdPSyncConfig) staleAfter() time.Duration {
	if c.StaleAfter > 0 {
		return c.StaleAfter
	}
	return time.Hour
}

// RunIdPSyncLoop periodically validates active sessions against the IdP.
// It checks if users are still valid, syncs group memberships, and revokes
// sessions for deactivated users.
func RunIdPSyncLoop(ctx context.Context, cfg IdPSyncConfig, pool db.Pool,
	oidc *OIDCProvider, secrets secret.SecretStore, enforcer casbin.IEnforcer) error {

	log.Info().
		Dur("interval", cfg.interval()).
		Dur("staleAfter", cfg.staleAfter()).
		Msg("idpsync: loop started")

	ticker := time.NewTicker(cfg.interval())
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("idpsync: loop stopped")
			return nil
		case <-ticker.C:
			syncCycle(ctx, cfg, pool, oidc, secrets, enforcer)
		}
	}
}

func syncCycle(ctx context.Context, cfg IdPSyncConfig, pool db.Pool,
	oidc *OIDCProvider, secrets secret.SecretStore, enforcer casbin.IEnforcer) {

	q := db.New(pool)

	// Fetch sessions that need syncing.
	sessions, err := q.ListSessionsForSync(ctx, db.ListSessionsForSyncParams{
		Secs:  cfg.staleAfter().Seconds(),
		Limit: cfg.batchSize(),
	})
	if err != nil {
		log.Error().Err(err).Msg("idpsync: failed to list sessions for sync")
		return
	}

	if len(sessions) == 0 {
		return
	}

	log.Info().Int("count", len(sessions)).Msg("idpsync: syncing sessions")

	synced, revoked := 0, 0
	for _, sess := range sessions {
		err := syncSession(ctx, q, pool, oidc, secrets, enforcer, sess)
		if err != nil {
			log.Warn().Err(err).Str("user", sess.Email).Msg("idpsync: session sync failed, revoking")
			if revokeErr := q.RevokeUserSessions(ctx, sess.UserID); revokeErr != nil {
				log.Error().Err(revokeErr).Str("user", sess.Email).Msg("idpsync: failed to revoke sessions")
			}
			revoked++
		} else {
			if syncErr := q.UpdateSessionSyncedAt(ctx, sess.ID); syncErr != nil {
				log.Warn().Err(syncErr).Str("session", sess.ID).Msg("idpsync: failed to update synced_at")
			}
			synced++
		}
	}

	// Cleanup expired sessions.
	if cleanErr := q.DeleteExpiredSessions(ctx); cleanErr != nil {
		log.Warn().Err(cleanErr).Msg("idpsync: failed to cleanup expired sessions")
	}

	log.Info().Int("synced", synced).Int("revoked", revoked).Msg("idpsync: cycle complete")
}

func syncSession(ctx context.Context, q *db.Queries, pool db.Pool,
	oidc *OIDCProvider, secrets secret.SecretStore, enforcer casbin.IEnforcer,
	sess db.ListSessionsForSyncRow) error {

	if oidc == nil {
		return fmt.Errorf("OIDC provider not configured")
	}

	// Decrypt the stored IdP token.
	var idpToken oauth2.Token
	if sess.IdpTokenEnc == nil {
		return fmt.Errorf("no IdP token stored")
	}

	// The IdP token is stored as JSON, optionally encrypted.
	// For now, assume JSON encoding (encryption can be added later via secret store).
	if err := json.Unmarshal(sess.IdpTokenEnc, &idpToken); err != nil {
		return fmt.Errorf("unmarshal IdP token: %w", err)
	}

	// Use the stored token to call UserInfo.
	// The token source will automatically refresh the token if expired.
	tokenSource := oidc.TokenSource(ctx, &idpToken)
	newToken, err := tokenSource.Token()
	if err != nil {
		return fmt.Errorf("IdP token refresh failed (user may be deactivated): %w", err)
	}

	// Call UserInfo with the (possibly refreshed) token.
	claims, err := oidc.UserInfo(ctx, newToken)
	if err != nil {
		return fmt.Errorf("UserInfo failed: %w", err)
	}

	// If the token was rotated by the IdP, save the new one.
	if newToken.AccessToken != idpToken.AccessToken {
		tokenJSON, _ := json.Marshal(newToken)
		if err := q.UpdateSessionIdpToken(ctx, db.UpdateSessionIdpTokenParams{
			ID:          sess.ID,
			IdpTokenEnc: tokenJSON,
		}); err != nil {
			log.Warn().Err(err).Str("session", sess.ID).Msg("idpsync: failed to update IdP token")
		}
	}

	// Sync group memberships (additive).
	if len(claims.Groups) > 0 {
		if err := syncTeams(ctx, q, sess.OrgID, sess.UserID, claims.Groups); err != nil {
			log.Warn().Err(err).Str("user", sess.Email).Msg("idpsync: team sync failed")
		}

		// Regenerate Casbin policies for this user.
		if err := RegenerateForSubject(ctx, q, pool, enforcer, sess.Email); err != nil {
			log.Warn().Err(err).Str("user", sess.Email).Msg("idpsync: policy regen failed")
		}
	}

	return nil
}
