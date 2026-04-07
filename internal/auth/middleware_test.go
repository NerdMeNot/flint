package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/NerdMeNot/flint/internal/auth"
)

func TestMiddleware_ValidToken(t *testing.T) {
	sm := auth.NewSessionManager(testSessionConfig())
	claims := &auth.Claims{Subject: "user-1", Email: "u@test.com", OrgID: "org1"}
	token, _ := sm.CreateSession(claims)

	var gotClaims *auth.Claims

	handler := auth.Middleware(sm)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotClaims = auth.ClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if gotClaims == nil || gotClaims.Email != "u@test.com" {
		t.Errorf("claims = %+v", gotClaims)
	}
}

func TestMiddleware_MissingHeader(t *testing.T) {
	sm := auth.NewSessionManager(testSessionConfig())
	handler := auth.Middleware(sm)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestMiddleware_InvalidFormat(t *testing.T) {
	sm := auth.NewSessionManager(testSessionConfig())
	handler := auth.Middleware(sm)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestMiddleware_InvalidToken(t *testing.T) {
	sm := auth.NewSessionManager(testSessionConfig())
	handler := auth.Middleware(sm)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	req.Header.Set("Authorization", "Bearer invalid.jwt.token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestClaimsFromContext_Nil(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	claims := auth.ClaimsFromContext(req.Context())
	if claims != nil {
		t.Errorf("expected nil claims for unauthenticated context, got %+v", claims)
	}
}
