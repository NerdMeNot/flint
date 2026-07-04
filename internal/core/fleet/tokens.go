package fleet

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
)

// MintToken generates a 32-byte random token, returned as plaintext with its
// sha256 hash — the plaintext goes to exactly one place (a bootstrap payload,
// a join-token response, a registration response) and only the hash is stored.
func MintToken() (plaintext, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", err
	}
	plaintext = hex.EncodeToString(raw)
	return plaintext, HashToken(plaintext), nil
}

// HashToken returns the hex sha256 of a token — the only form persisted.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
