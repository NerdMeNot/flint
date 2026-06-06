package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/NerdMeNot/flint/internal/core/flinterr"
	"github.com/golang-jwt/jwt/v5"
)

// flintClaims is the JWT claims structure for Flint sessions.
// Note: Role is no longer stored in the JWT. The JWT provides identity only;
// authorization is handled by Casbin at request time.
type flintClaims struct {
	jwt.RegisteredClaims
	Email      string   `json:"email"`
	Name       string   `json:"name,omitempty"`
	OrgID      string   `json:"org_id"`
	Role       string   `json:"role,omitempty"` // legacy — ignored on validation, kept for backward compat
	Groups     []string `json:"groups,omitempty"`
	ExternalID string   `json:"external_id"`
	Provider   string   `json:"provider"`
}

// Sessions is the interface for JWT session management. *SessionManager
// implements it; tests can mock it to avoid real JWT signing.
type Sessions interface {
	CreateSession(claims *Claims) (string, error)
	ValidateSession(tokenString string) (*Claims, error)
}

// SessionManager creates and validates JWT session tokens.
type SessionManager struct {
	config SessionConfig
}

// compile-time check
var _ Sessions = (*SessionManager)(nil)

// NewSessionManager creates a SessionManager.
func NewSessionManager(config SessionConfig) *SessionManager {
	return &SessionManager{config: config}
}

// CreateSession creates a signed JWT from authenticated claims.
// The JWT is identity-only — no role embedded. Casbin handles authorization.
func (s *SessionManager) CreateSession(claims *Claims) (string, error) {
	now := time.Now()

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, &flintClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    s.config.Issuer,
			Subject:   claims.Subject,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.config.sessionDuration())),
		},
		Email:      claims.Email,
		Name:       claims.Name,
		OrgID:      claims.OrgID,
		Groups:     claims.Groups,
		ExternalID: claims.ExternalID,
		Provider:   claims.Provider,
	})

	signed, err := token.SignedString(s.config.SigningKey)
	if err != nil {
		return "", flinterr.WrapInternal("failed to sign session token", err)
	}

	return signed, nil
}

// ValidateSession parses and validates a JWT, returning the embedded claims.
// Role is no longer returned — authorization is via Casbin, not the JWT.
func (s *SessionManager) ValidateSession(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &flintClaims{}, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return s.config.SigningKey, nil
	})
	if err != nil {
		return nil, flinterr.WrapInvalidInput("invalid session token", err)
	}

	fc, ok := token.Claims.(*flintClaims)
	if !ok || !token.Valid {
		return nil, flinterr.NewInvalidInput("invalid session token claims")
	}

	claims := &Claims{
		Subject:    fc.Subject,
		Email:      fc.Email,
		Name:       fc.Name,
		Groups:     fc.Groups,
		OrgID:      fc.OrgID,
		ExternalID: fc.ExternalID,
		Provider:   fc.Provider,
	}
	if fc.IssuedAt != nil {
		claims.IssuedAt = fc.IssuedAt.Time
	}
	if fc.ExpiresAt != nil {
		claims.ExpiresAt = fc.ExpiresAt.Time
	}

	return claims, nil
}

// GenerateRefreshToken creates a cryptographically random refresh token.
// Returns the raw token (to send to client) and its SHA-256 hash (to store in DB).
func GenerateRefreshToken() (raw string, hash string, err error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}
	raw = "flint_rt_" + hex.EncodeToString(bytes)
	hash = HashToken(raw)
	return raw, hash, nil
}

// HashToken returns the SHA-256 hex digest of a token string.
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}
