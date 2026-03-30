package forge

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// GitHubAppAuth handles GitHub App authentication — generating JWTs from the
// App's private key and exchanging them for short-lived installation tokens.
//
// Installation tokens expire after 1 hour. This struct caches the token and
// refreshes it 5 minutes before expiry.
type GitHubAppAuth struct {
	AppID          string
	InstallationID string
	PrivateKey     *rsa.PrivateKey
	BaseURL        string
	Client         *http.Client

	mu          sync.Mutex
	cachedToken string
	tokenExpiry time.Time
}

// NewGitHubAppAuth creates a new GitHubAppAuth from a PEM-encoded private key.
func NewGitHubAppAuth(appID, installationID string, privateKeyPEM []byte) (*GitHubAppAuth, error) {
	block, _ := pem.Decode(privateKeyPEM)
	if block == nil {
		return nil, fmt.Errorf("forge: failed to decode PEM block from private key")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS8 format.
		pkcs8Key, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			return nil, fmt.Errorf("forge: failed to parse private key (tried PKCS1 and PKCS8): %w", err)
		}
		var ok bool
		key, ok = pkcs8Key.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("forge: private key is not RSA")
		}
	}

	return &GitHubAppAuth{
		AppID:          appID,
		InstallationID: installationID,
		PrivateKey:     key,
	}, nil
}

// Token returns a valid installation token, refreshing if necessary.
// Safe for concurrent use.
func (a *GitHubAppAuth) Token() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Return cached token if still valid (with 5 min buffer).
	if a.cachedToken != "" && time.Now().Add(5*time.Minute).Before(a.tokenExpiry) {
		return a.cachedToken, nil
	}

	// Generate a new App JWT.
	appJWT, err := a.generateAppJWT()
	if err != nil {
		return "", err
	}

	// Exchange for an installation token.
	token, expiry, err := a.exchangeForInstallationToken(appJWT)
	if err != nil {
		return "", err
	}

	a.cachedToken = token
	a.tokenExpiry = expiry

	return token, nil
}

// generateAppJWT creates a short-lived JWT signed with the App's private key.
// GitHub requires the JWT to expire within 10 minutes.
func (a *GitHubAppAuth) generateAppJWT() (string, error) {
	now := time.Now()
	claims := jwt.RegisteredClaims{
		Issuer:    a.AppID,
		IssuedAt:  jwt.NewNumericDate(now.Add(-60 * time.Second)), // clock skew buffer
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),   // max 10 min
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(a.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("forge: failed to sign App JWT: %w", err)
	}

	return signed, nil
}

// exchangeForInstallationToken calls the GitHub API to get an installation token.
func (a *GitHubAppAuth) exchangeForInstallationToken(appJWT string) (string, time.Time, error) {
	baseURL := a.BaseURL
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}

	url := fmt.Sprintf("%s/app/installations/%s/access_tokens", baseURL, a.InstallationID)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("forge: failed to create token request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := a.Client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("%w: failed to exchange for installation token: %v", ErrTransient, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 201 {
		body, _ := io.ReadAll(resp.Body)
		return "", time.Time{}, fmt.Errorf("forge: installation token exchange returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", time.Time{}, fmt.Errorf("forge: failed to decode token response: %w", err)
	}

	return result.Token, result.ExpiresAt, nil
}
