package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

// totpPeriodSeconds is the TOTP step size; must match GenerateTOTPSecret.
const totpPeriodSeconds = 30

// TOTPPeriod returns the TOTP time-step counter for t (unix seconds / period).
// Used for replay protection: a code is rejected if its period is not strictly
// greater than the last accepted one for that user.
func TOTPPeriod(t time.Time) int64 {
	return t.Unix() / totpPeriodSeconds
}

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
		code, err := generateRecoveryCode()
		if err != nil {
			return nil, nil, err
		}
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

// generateRecoveryCode creates a human-readable recovery code
// (e.g. "A3F8-C21B-9E4D-7F02").
//
// 8 bytes, not 4. The previous 32-bit code was the weakest way into an account
// with MFA on — weaker than the 10^6 TOTP space it backs up — and it was
// submitted through the same endpoint. A rand failure is fatal rather than
// silently yielding an all-zero code: this is the credential of last resort.
func generateRecoveryCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate recovery code: %w", err)
	}
	h := strings.ToUpper(hex.EncodeToString(b))
	return h[0:4] + "-" + h[4:8] + "-" + h[8:12] + "-" + h[12:16], nil
}
