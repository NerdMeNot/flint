package auth

import (
	"context"
	"net/http"
	"strings"
)

type contextKey string

const (
	claimsKey contextKey = "flint_claims"
)

// Middleware extracts and validates the Bearer token from the Authorization header,
// storing the claims in the request context. Role is NOT stored — authorization
// is handled by Casbin at the server middleware layer.
func Middleware(sm *SessionManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "missing authorization header", http.StatusUnauthorized)
				return
			}

			token := strings.TrimPrefix(authHeader, "Bearer ")
			if token == authHeader {
				http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
				return
			}

			claims, err := sm.ValidateSession(token)
			if err != nil {
				http.Error(w, "invalid or expired token", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), claimsKey, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// ClaimsFromContext retrieves the authenticated claims from the context.
// Returns nil if no claims are present (unauthenticated request).
func ClaimsFromContext(ctx context.Context) *Claims {
	claims, _ := ctx.Value(claimsKey).(*Claims)
	return claims
}
