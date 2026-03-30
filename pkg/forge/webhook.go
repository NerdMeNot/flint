package forge

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// VerifyHMACSHA256 verifies an HMAC-SHA256 signature against a payload.
// The signature should be in the format "sha256=<hex>".
func VerifyHMACSHA256(payload []byte, signature, secret string) error {
	sig := strings.TrimPrefix(signature, "sha256=")
	sigBytes, err := hex.DecodeString(sig)
	if err != nil {
		return fmt.Errorf("%w: invalid signature encoding", ErrWebhookInvalid)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := mac.Sum(nil)

	if !hmac.Equal(sigBytes, expected) {
		return ErrSignatureMismatch
	}

	return nil
}
