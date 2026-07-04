package config

import (
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// ValidationError collects all validation failures.
type ValidationError struct {
	Errors []string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("config validation failed:\n  - %s", strings.Join(e.Errors, "\n  - "))
}

func (e *ValidationError) add(msg string) {
	e.Errors = append(e.Errors, msg)
}

func (e *ValidationError) hasErrors() bool {
	return len(e.Errors) > 0
}

// Validate checks the config for required fields and invalid values.
// Call this after Load() before using the config. Returns nil if valid.
func Validate(cfg *Config, component string) error {
	v := &ValidationError{}

	// Database is required for server and dispatch.
	if component == "server" || component == "dispatch" {
		if cfg.Database.Host == "" {
			v.add("database.host is required")
		}
		if cfg.Database.Database == "" {
			v.add("database.database is required")
		}
		if cfg.Database.User == "" {
			v.add("database.user is required")
		}
	}

	// Engine uses Postgres directly — no separate Temporal validation needed.

	// JWT secret signs both web sessions and the engine's task tokens. The
	// dispatch loop mints task tokens and the server verifies them, so BOTH need
	// it and it must be the same value across components.
	if component == "server" || component == "dispatch" {
		if cfg.Auth.JWT.Secret == "" {
			v.add("auth.jwt.secret is required")
		} else if len(cfg.Auth.JWT.Secret) < 32 {
			v.add("auth.jwt.secret must be at least 32 characters")
		}
	}

	// Server-specific.
	if component == "server" {
		// Master key is required: forge connections, SSO config, and the secret
		// store are all envelope-encrypted with it.
		if cfg.Encryption.MasterKey == "" {
			v.add("encryption.masterKey is required")
		} else {
			key, err := hex.DecodeString(cfg.Encryption.MasterKey)
			if err != nil {
				v.add("encryption.masterKey must be valid hex")
			} else if len(key) != 32 {
				v.add(fmt.Sprintf("encryption.masterKey must be 32 bytes (64 hex chars), got %d bytes", len(key)))
			}
		}
	}

	// Dispatch sweep interval.
	if component == "dispatch" && cfg.Engine.SweepInterval > 0 && cfg.Engine.SweepInterval < 30*time.Second {
		v.add(fmt.Sprintf("engine.sweepInterval minimum is 30s, got %s", cfg.Engine.SweepInterval))
	}

	// Storage validation.
	if cfg.Storage.Mode != "" && cfg.Storage.Mode != "s3" && cfg.Storage.Mode != "filesystem" {
		v.add(fmt.Sprintf("storage.mode must be 's3' or 'filesystem', got %q", cfg.Storage.Mode))
	}
	if cfg.Storage.Mode == "s3" {
		if cfg.Storage.S3.Bucket == "" {
			v.add("storage.s3.bucket is required when storage.mode is 's3'")
		}
		if cfg.Storage.S3.Region == "" {
			v.add("storage.s3.region is required when storage.mode is 's3'")
		}
	}

	if v.hasErrors() {
		return v
	}
	return nil
}
