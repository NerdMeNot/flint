package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters — tuned for server-side hashing.
// These match OWASP recommendations for Argon2id.
const (
	argonTime    = 3         // iterations
	argonMemory  = 64 * 1024 // 64 MB
	argonThreads = 4         // parallelism
	argonKeyLen  = 32        // output key length
	argonSaltLen = 16        // salt length
)

// HashPassword hashes a password using Argon2id.
// Returns a PHC-format string: $argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash),
	), nil
}

// VerifyPassword checks a password against an Argon2id hash in PHC format.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}

	var memory uint32
	var time uint32
	var threads uint8
	_, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads)
	if err != nil {
		return false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}

	expectedHash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false
	}

	actualHash := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(expectedHash)))

	return subtle.ConstantTimeCompare(actualHash, expectedHash) == 1
}

// GenerateRandomPassword creates a cryptographically random password.
//
// A rand failure is reported: this generates the bootstrap admin password, and
// an all-zero fallback would be a fixed, publicly-known credential on a fresh
// install.
func GenerateRandomPassword() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating password: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// dummyPasswordHash is a valid Argon2id hash of a value nobody knows. It exists
// so the "user not found" path can pay the same ~100ms of hashing as a real
// verification.
//
// Without it, a miss returned immediately and a hit did not, which is a clean
// account-enumeration oracle over the login endpoint — the more so because the
// hashing parameters are deliberately expensive, making the difference easy to
// measure over a noisy network.
var dummyPasswordHash = sync.OnceValue(func() string {
	pw, err := GenerateRandomPassword()
	if err != nil {
		return fallbackDummyHash()
	}
	h, err := HashPassword(pw)
	if err != nil {
		return fallbackDummyHash()
	}
	return h
})

// fallbackDummyHash is a well-formed Argon2id hash with an all-zero digest. It
// can never match a real password — no input hashes to zero — but it parses, so
// VerifyPassword still does the full derivation and the timing property holds
// even if the CSPRNG was unavailable at startup.
func fallbackDummyHash() string {
	return "$argon2id$v=19$m=65536,t=3,p=4$" +
		base64.RawStdEncoding.EncodeToString(make([]byte, argonSaltLen)) + "$" +
		base64.RawStdEncoding.EncodeToString(make([]byte, argonKeyLen))
}

// VerifyAgainstDummyHash performs a full Argon2id verification against a hash
// with no known preimage. Call it on paths that reject before reaching a real
// hash, so the response time does not reveal whether the account exists.
func VerifyAgainstDummyHash(password string) {
	_ = VerifyPassword(password, dummyPasswordHash())
}
