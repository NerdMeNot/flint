package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// Load reads configuration from a YAML file and environment variables.
// Environment variables use the FLINT_ prefix with underscores replacing dots:
//
//	server.port → FLINT_SERVER_PORT
//	database.host → FLINT_DATABASE_HOST
//	temporal.hostPort → FLINT_TEMPORAL_HOSTPORT
func Load(path string) (*Config, error) {
	v := viper.New()

	// Defaults.
	v.SetDefault("server.port", 8080)
	v.SetDefault("database.port", 5432)
	v.SetDefault("database.sslMode", "disable")
	v.SetDefault("worker.jobNamespace", "flint-jobs")
	v.SetDefault("worker.defaultRunnerPool", "standard")
	v.SetDefault("storage.mode", "filesystem")
	v.SetDefault("storage.filesystem.path", "/tmp/flint-logs")
	v.SetDefault("encryption.masterKeyVersion", 1)

	// Environment variables.
	v.SetEnvPrefix("FLINT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Config file.
	if path != "" {
		v.SetConfigFile(path)
		if err := v.ReadInConfig(); err != nil {
			return nil, fmt.Errorf("config: failed to read %s: %w", path, err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: failed to unmarshal: %w", err)
	}

	return &cfg, nil
}
