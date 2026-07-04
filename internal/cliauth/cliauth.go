// Package cliauth manages CLI/TUI credentials: the device-flow login, the
// ~/.config/flint/credentials file, and access-token refresh. Both `flint
// login` and the TUI client build on it.
package cliauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Credentials is the persisted CLI identity for one Flint server.
type Credentials struct {
	ServerURL string `json:"serverUrl"`
	// Token is a personal access token (flint_pat_*) or a session access JWT.
	Token string `json:"token"`
	// RefreshToken rotates session JWTs; empty for PATs (which don't expire
	// the same way).
	RefreshToken string `json:"refreshToken,omitempty"`
}

// IsPAT reports whether the stored token is a personal access token.
func (c *Credentials) IsPAT() bool { return strings.HasPrefix(c.Token, "flint_pat_") }

// Path returns the credentials file location (FLINT_CREDENTIALS overrides).
func Path() (string, error) {
	if p := os.Getenv("FLINT_CREDENTIALS"); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine config dir: %w", err)
	}
	return filepath.Join(dir, "flint", "credentials"), nil
}

// Load reads the stored credentials. A FLINT_TOKEN env var (with optional
// FLINT_SERVER_URL) takes precedence — the CI/scripting path.
func Load() (*Credentials, error) {
	if tok := os.Getenv("FLINT_TOKEN"); tok != "" {
		return &Credentials{
			ServerURL: strings.TrimRight(defaultStr(os.Getenv("FLINT_SERVER_URL"), "http://localhost:8080"), "/"),
			Token:     tok,
		}, nil
	}
	p, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("not logged in — run `flint login`")
		}
		return nil, err
	}
	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("corrupt credentials file %s: %w", p, err)
	}
	return &c, nil
}

// Save writes the credentials with owner-only permissions.
func Save(c *Credentials) error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// Delete removes the stored credentials (logout).
func Delete() error {
	p, err := Path()
	if err != nil {
		return err
	}
	err = os.Remove(p)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// Refresh rotates the access token using the refresh token and persists the
// result. Returns an error for PATs (nothing to refresh) or expired sessions.
func Refresh(ctx context.Context, c *Credentials) error {
	if c.RefreshToken == "" {
		return fmt.Errorf("token expired and no refresh token stored — run `flint login`")
	}
	body, _ := json.Marshal(map[string]string{"refreshToken": c.RefreshToken})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.ServerURL+"/auth/refresh", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("session expired — run `flint login`")
	}
	var out struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return err
	}
	c.Token = out.AccessToken
	c.RefreshToken = out.RefreshToken
	return Save(c)
}

// DeviceLogin runs the OAuth-style device flow against serverURL: it prints
// the user code + verification URL via prompt, polls until the user approves
// in a browser, and returns the credentials (already persisted).
func DeviceLogin(ctx context.Context, serverURL string, prompt func(userCode, verificationURI string)) (*Credentials, error) {
	serverURL = strings.TrimRight(serverURL, "/")
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Post(serverURL+"/auth/device/code", "application/json", strings.NewReader("{}"))
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %w", serverURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("device code request failed (%d): %s", resp.StatusCode, string(b))
	}
	var dc struct {
		DeviceCode      string `json:"deviceCode"`
		UserCode        string `json:"userCode"`
		VerificationURI string `json:"verificationUri"`
		ExpiresIn       int    `json:"expiresIn"`
		Interval        int    `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&dc); err != nil {
		return nil, err
	}
	prompt(dc.UserCode, dc.VerificationURI)

	interval := time.Duration(max(dc.Interval, 1)) * time.Second
	deadline := time.Now().Add(time.Duration(max(dc.ExpiresIn, 60)) * time.Second)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}

		body, _ := json.Marshal(map[string]string{"deviceCode": dc.DeviceCode})
		tokResp, err := client.Post(serverURL+"/auth/device/token", "application/json", bytes.NewReader(body))
		if err != nil {
			continue // transient — keep polling
		}
		raw, _ := io.ReadAll(io.LimitReader(tokResp.Body, 8192))
		tokResp.Body.Close()

		if tokResp.StatusCode == http.StatusOK {
			var tok struct {
				AccessToken  string `json:"accessToken"`
				RefreshToken string `json:"refreshToken"`
			}
			if err := json.Unmarshal(raw, &tok); err != nil {
				return nil, err
			}
			creds := &Credentials{ServerURL: serverURL, Token: tok.AccessToken, RefreshToken: tok.RefreshToken}
			if err := Save(creds); err != nil {
				return nil, fmt.Errorf("login succeeded but saving credentials failed: %w", err)
			}
			return creds, nil
		}

		var apiErr struct {
			Error struct {
				Code string `json:"code"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &apiErr)
		switch apiErr.Error.Code {
		case "AUTHORIZATION_PENDING":
			continue
		case "SLOW_DOWN":
			interval += 2 * time.Second
			continue
		case "EXPIRED_TOKEN":
			return nil, fmt.Errorf("device code expired — run `flint login` again")
		default:
			return nil, fmt.Errorf("device authorization failed: %s", strings.TrimSpace(string(raw)))
		}
	}
	return nil, fmt.Errorf("device code expired — run `flint login` again")
}

func defaultStr(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
