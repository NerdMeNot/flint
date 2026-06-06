package config_test

import (
	"strings"
	"testing"

	"github.com/NerdMeNot/flint/internal/platform/config"
)

func validServerConfig() *config.Config {
	return &config.Config{
		Database: config.DatabaseConfig{
			Host:     "localhost",
			Database: "flint",
			User:     "flint",
		},
		Auth: config.AuthConfig{
			JWT: config.JWTConfig{
				Secret: "this-is-a-secret-that-is-at-least-32-chars",
			},
		},
		Encryption: config.EncryptionConfig{
			// 32-byte (64 hex char) master key.
			MasterKey: strings.Repeat("ab", 32),
		},
		Server: config.ServerConfig{
			InternalToken: "internal-token-at-least-16-chars",
		},
		Worker: config.WorkerConfig{
			AgentImage: "ghcr.io/NerdMeNot/flint-agent:latest",
		},
	}
}

func TestValidate_ValidServer(t *testing.T) {
	cfg := validServerConfig()
	if err := config.Validate(cfg, "server"); err != nil {
		t.Errorf("expected valid config, got: %v", err)
	}
}

func TestValidate_MissingDBHost(t *testing.T) {
	cfg := validServerConfig()
	cfg.Database.Host = ""

	err := config.Validate(cfg, "server")
	if err == nil {
		t.Fatal("expected error for missing db host")
	}
	if !strings.Contains(err.Error(), "database.host") {
		t.Errorf("error should mention database.host: %v", err)
	}
}

func TestValidate_ShortJWTSecret(t *testing.T) {
	cfg := validServerConfig()
	cfg.Auth.JWT.Secret = "short"

	err := config.Validate(cfg, "server")
	if err == nil {
		t.Fatal("expected error for short JWT secret")
	}
	if !strings.Contains(err.Error(), "32 characters") {
		t.Errorf("error should mention length: %v", err)
	}
}

func TestValidate_InvalidMasterKey(t *testing.T) {
	cfg := validServerConfig()
	cfg.Encryption.MasterKey = "not-hex"

	err := config.Validate(cfg, "server")
	if err == nil {
		t.Fatal("expected error for invalid hex master key")
	}
}

func TestValidate_WrongLengthMasterKey(t *testing.T) {
	cfg := validServerConfig()
	cfg.Encryption.MasterKey = "aabbccdd" // only 4 bytes

	err := config.Validate(cfg, "server")
	if err == nil {
		t.Fatal("expected error for wrong length master key")
	}
	if !strings.Contains(err.Error(), "32 bytes") {
		t.Errorf("error should mention 32 bytes: %v", err)
	}
}

func TestValidate_InvalidStorageMode(t *testing.T) {
	cfg := validServerConfig()
	cfg.Storage.Mode = "gcs"

	err := config.Validate(cfg, "server")
	if err == nil {
		t.Fatal("expected error for invalid storage mode")
	}
}

func TestValidate_S3MissingBucket(t *testing.T) {
	cfg := validServerConfig()
	cfg.Storage.Mode = "s3"
	cfg.Storage.S3.Region = "us-east-1"

	err := config.Validate(cfg, "server")
	if err == nil {
		t.Fatal("expected error for missing S3 bucket")
	}
}

func TestValidate_WorkerMissingAgentImage(t *testing.T) {
	cfg := validServerConfig()
	cfg.Worker.AgentImage = ""

	err := config.Validate(cfg, "worker")
	if err == nil {
		t.Fatal("expected error for missing agent image")
	}
}

func TestValidate_MultipleErrors(t *testing.T) {
	cfg := &config.Config{} // everything missing

	err := config.Validate(cfg, "server")
	if err == nil {
		t.Fatal("expected errors for empty config")
	}

	ve, ok := err.(*config.ValidationError)
	if !ok {
		t.Fatalf("expected *ValidationError, got %T", err)
	}
	if len(ve.Errors) < 3 {
		t.Errorf("expected at least 3 errors, got %d: %v", len(ve.Errors), ve.Errors)
	}
}
