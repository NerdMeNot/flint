package dbkit

import "fmt"

// Config holds PostgreSQL connection parameters.
type Config struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string
	SSLMode  string // disable, require, verify-ca, verify-full

	// Pool settings.
	MaxConns int32
	MinConns int32
}

// DSN returns a PostgreSQL connection string.
func (c *Config) DSN() string {
	sslMode := c.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}
	port := c.Port
	if port == 0 {
		port = 5432
	}
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		c.User, c.Password, c.Host, port, c.Database, sslMode,
	)
}
