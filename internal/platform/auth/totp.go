package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

// GenerateTOTPSecret creates a new TOTP secret for the given user.
// Returns the OTP key (contains secret + QR code URL).
func GenerateTOTPSecret(email, issuer string) (*otp.Key, error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: email,
		Period:      30,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return nil, fmt.Errorf("generate TOTP secret: %w", err)
	}
	return key, nil
}

// ValidateTOTPCode checks a 6-digit TOTP code against the secret.
// Allows a 1-step window (30 seconds before/after) to account for clock skew.
func ValidateTOTPCode(secret, code string) bool {
	return totp.Validate(code, secret)
}

// GenerateRecoveryCodes creates a set of one-time recovery codes.
// Returns the raw codes (to show the user once) and bcrypt hashes (to store).
func GenerateRecoveryCodes(count int) (raw []string, hashed []string, err error) {
	raw = make([]string, count)
	hashed = make([]string, count)

	for i := 0; i < count; i++ {
		code := generateRecoveryCode()
		raw[i] = code

		hash, err := bcrypt.GenerateFromPassword([]byte(code), bcrypt.DefaultCost)
		if err != nil {
			return nil, nil, fmt.Errorf("hash recovery code: %w", err)
		}
		hashed[i] = string(hash)
	}

	return raw, hashed, nil
}

// ValidateRecoveryCode checks a recovery code against the stored hashes.
// Returns the remaining hashes (with the used one removed) and whether the code was valid.
// Recovery codes are single-use.
func ValidateRecoveryCode(code string, hashed []string) (remaining []string, valid bool) {
	for i, h := range hashed {
		if bcrypt.CompareHashAndPassword([]byte(h), []byte(code)) == nil {
			// Valid — remove this code from the list (single-use).
			remaining = make([]string, 0, len(hashed)-1)
			remaining = append(remaining, hashed[:i]...)
			remaining = append(remaining, hashed[i+1:]...)
			return remaining, true
		}
	}
	return hashed, false
}

// generateRecoveryCode creates a human-readable recovery code (e.g., "a3f8-c21b").
func generateRecoveryCode() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	code := hex.EncodeToString(b)
	return strings.ToUpper(code[:4] + "-" + code[4:])
}
