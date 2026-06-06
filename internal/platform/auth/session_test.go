package auth_test

import (
	"testing"
	"time"

	"github.com/NerdMeNot/flint/internal/core/flinterr"
	"github.com/NerdMeNot/flint/internal/platform/auth"
)

func testSessionConfig() auth.SessionConfig {
	return auth.SessionConfig{
		SigningKey:      []byte("test-secret-key-32-bytes-long!!!"),
		Issuer:          "https://flint.test",
		SessionDuration: 1 * time.Hour,
		RefreshDuration: 24 * time.Hour,
	}
}

func TestSessionManager_RoundTrip(t *testing.T) {
	sm := auth.NewSessionManager(testSessionConfig())

	claims := &auth.Claims{
		Subject:    "user-123",
		Email:      "dev@acme.com",
		Name:       "Dev User",
		Groups:     []string{"engineering", "platform"},
		OrgID:      "org-abc",
		ExternalID: "idp|user-123",
		Provider:   "oidc",
	}

	token, err := sm.CreateSession(claims)
	if err != nil {
		t.Fatalf("CreateSession() error: %v", err)
	}
	if token == "" {
		t.Fatal("CreateSession() returned empty token")
	}

	got, err := sm.ValidateSession(token)
	if err != nil {
		t.Fatalf("ValidateSession() error: %v", err)
	}

	if got.Subject != "user-123" {
		t.Errorf("Subject = %q", got.Subject)
	}
	if got.Email != "dev@acme.com" {
		t.Errorf("Email = %q", got.Email)
	}
	if got.Name != "Dev User" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.OrgID != "org-abc" {
		t.Errorf("OrgID = %q", got.OrgID)
	}
	if got.ExternalID != "idp|user-123" {
		t.Errorf("ExternalID = %q", got.ExternalID)
	}
	if got.Provider != "oidc" {
		t.Errorf("Provider = %q", got.Provider)
	}
	if len(got.Groups) != 2 {
		t.Errorf("Groups = %v", got.Groups)
	}
	if got.IssuedAt.IsZero() {
		t.Error("IssuedAt is zero")
	}
	if got.ExpiresAt.IsZero() {
		t.Error("ExpiresAt is zero")
	}
}

func TestSessionManager_InvalidToken(t *testing.T) {
	sm := auth.NewSessionManager(testSessionConfig())

	tests := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"garbage", "not.a.jwt"},
		{"truncated", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ0ZXN0In0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := sm.ValidateSession(tt.token)
			if err == nil {
				t.Fatal("expected error for invalid token")
			}
			if !flinterr.IsKind(err, flinterr.KindInvalidInput) {
				t.Errorf("expected KindInvalidInput, got: %v", err)
			}
		})
	}
}

func TestSessionManager_WrongKey(t *testing.T) {
	cfg1 := testSessionConfig()
	cfg2 := testSessionConfig()
	cfg2.SigningKey = []byte("different-key-32-bytes-long!!!!!")

	sm1 := auth.NewSessionManager(cfg1)
	sm2 := auth.NewSessionManager(cfg2)

	claims := &auth.Claims{Subject: "user", Email: "u@test.com", OrgID: "org1"}
	token, _ := sm1.CreateSession(claims)

	_, err := sm2.ValidateSession(token)
	if err == nil {
		t.Fatal("expected error when validating with wrong key")
	}
}

func TestSessionManager_ExpiredToken(t *testing.T) {
	cfg := testSessionConfig()
	cfg.SessionDuration = 1 * time.Millisecond

	sm := auth.NewSessionManager(cfg)
	claims := &auth.Claims{Subject: "user", Email: "u@test.com", OrgID: "org1"}

	token, err := sm.CreateSession(claims)
	if err != nil {
		t.Fatalf("CreateSession() error: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	_, err = sm.ValidateSession(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}
