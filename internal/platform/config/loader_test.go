package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/NerdMeNot/flint/internal/platform/config"
)

func TestLoad_Defaults(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Server.PortOrDefault() != 8080 {
		t.Errorf("Server.Port = %d, want 8080", cfg.Server.PortOrDefault())
	}
	if cfg.Database.PortOrDefault() != 5432 {
		t.Errorf("Database.Port = %d, want 5432", cfg.Database.PortOrDefault())
	}
	if cfg.Worker.JobNamespaceOrDefault() != "flint-jobs" {
		t.Errorf("Worker.JobNamespace = %q", cfg.Worker.JobNamespaceOrDefault())
	}
}

func TestLoad_YAMLFile(t *testing.T) {
	yamlContent := `
server:
  port: 9090
  baseUrl: https://flint.example.com

database:
  host: db.example.com
  port: 5433
  database: flint_prod
  user: admin
  password: secret123
  sslMode: verify-full

`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Errorf("Server.Port = %d, want 9090", cfg.Server.Port)
	}
	if cfg.Server.BaseURL != "https://flint.example.com" {
		t.Errorf("Server.BaseURL = %q", cfg.Server.BaseURL)
	}
	if cfg.Database.Host != "db.example.com" {
		t.Errorf("Database.Host = %q", cfg.Database.Host)
	}
	if cfg.Database.Port != 5433 {
		t.Errorf("Database.Port = %d", cfg.Database.Port)
	}
	if cfg.Database.SSLMode != "verify-full" {
		t.Errorf("Database.SSLMode = %q", cfg.Database.SSLMode)
	}
	// Forge connections are managed in DB, not config file.
}

func TestLoad_EnvOverride(t *testing.T) {
	// Env vars override values that have defaults or are in the config file.
	t.Setenv("FLINT_SERVER_PORT", "3000")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	// server.port has a default, so env override works.
	if cfg.Server.Port != 3000 {
		t.Errorf("Server.Port = %d, want 3000", cfg.Server.Port)
	}
}

func TestLoad_EnvOverrideWithFile(t *testing.T) {
	// Env vars override values from config file.
	yamlContent := `
database:
  host: file-db.example.com
  port: 5432
`
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(yamlContent), 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	t.Setenv("FLINT_DATABASE_HOST", "env-db.example.com")

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.Database.Host != "env-db.example.com" {
		t.Errorf("Database.Host = %q, want env-db.example.com", cfg.Database.Host)
	}
}

func TestLoad_InvalidFile(t *testing.T) {
	_, err := config.Load("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestLoad_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yaml")
	if err := os.WriteFile(path, []byte("[[[invalid yaml"), 0o644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	_, err := config.Load(path)
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
}
