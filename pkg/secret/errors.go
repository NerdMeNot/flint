package secret

import "errors"

// Sentinel errors for secret operations.
var (
	ErrNotFound      = errors.New("secret: not found")
	ErrDecryptFailed = errors.New("secret: decryption failed")
	ErrInvalidKey    = errors.New("secret: invalid encryption key")
	ErrInvalidRef    = errors.New("secret: invalid secret reference")
)
