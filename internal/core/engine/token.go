package engine

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// TaskToken identifies a specific step execution for async completion.
// Encoded as base64(JSON), optionally followed by ".<base64 HMAC>" when a
// signing key is supplied. The signature binds the token to the run so an
// untrusted step pod cannot forge a token for a different workflow (which would
// let it read another run's secrets). The pod receives only its own pre-signed
// token; the signing key never leaves the server/worker.
type TaskToken struct {
	WorkflowID string `json:"w"`
	StepName   string `json:"s"`
	Attempt    int    `json:"a"`
}

// tokenMACDomain separates this HMAC use from any other use of the same key.
const tokenMACDomain = "flint-task-token:v1:"

// EncodeTaskToken serializes a task token to a string. When a non-empty signing
// key is supplied, the payload is signed with HMAC-SHA256 and the signature is
// appended as ".<mac>". The key is optional only so tests/dev can mint unsigned
// tokens; production always passes a key.
func EncodeTaskToken(t TaskToken, key ...[]byte) string {
	b, _ := json.Marshal(t)
	payload := base64.RawURLEncoding.EncodeToString(b)
	k := firstKey(key)
	if len(k) == 0 {
		return payload
	}
	return payload + "." + signTokenPayload(payload, k)
}

// DecodeTaskToken parses and (when a non-empty key is supplied) verifies a task
// token. With a key set, an unsigned or wrongly-signed token is rejected.
func DecodeTaskToken(s string, key ...[]byte) (TaskToken, error) {
	payload, sig, _ := strings.Cut(s, ".")

	if k := firstKey(key); len(k) > 0 {
		if sig == "" {
			return TaskToken{}, fmt.Errorf("engine: task token is not signed")
		}
		if !hmac.Equal([]byte(sig), []byte(signTokenPayload(payload, k))) {
			return TaskToken{}, fmt.Errorf("engine: task token signature is invalid")
		}
	}

	b, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return TaskToken{}, fmt.Errorf("engine: invalid task token encoding: %w", err)
	}
	var t TaskToken
	if err := json.Unmarshal(b, &t); err != nil {
		return TaskToken{}, fmt.Errorf("engine: invalid task token payload: %w", err)
	}
	if t.WorkflowID == "" || t.StepName == "" {
		return TaskToken{}, fmt.Errorf("engine: task token missing required fields")
	}
	return t, nil
}

// firstKey returns the first signing key from the variadic arg, or nil.
func firstKey(key [][]byte) []byte {
	if len(key) > 0 {
		return key[0]
	}
	return nil
}

func signTokenPayload(payload string, key []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(tokenMACDomain))
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
