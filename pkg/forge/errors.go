package forge

import "errors"

// Sentinel errors for forge operations.
var (
	ErrWebhookInvalid    = errors.New("forge: invalid webhook")
	ErrSignatureMismatch = errors.New("forge: signature mismatch")
	ErrNotFound          = errors.New("forge: resource not found")
	ErrTransient         = errors.New("forge: transient error")
	ErrUnsupportedEvent  = errors.New("forge: unsupported event type")
)
