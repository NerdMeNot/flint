package dbkit_test

import (
	"testing"

	"github.com/NerdMeNot/flint/internal/dbkit"
)

func TestConfig_DSN(t *testing.T) {
	tests := []struct {
		name string
		cfg  dbkit.Config
		want string
	}{
		{
			name: "basic",
			cfg: dbkit.Config{
				Host:     "localhost",
				Port:     5432,
				Database: "flint",
				User:     "flint",
				Password: "secret",
			},
			want: "postgres://flint:secret@localhost:5432/flint?sslmode=disable",
		},
		{
			name: "with ssl",
			cfg: dbkit.Config{
				Host:     "rds.example.com",
				Port:     5432,
				Database: "flint",
				User:     "admin",
				Password: "pass",
				SSLMode:  "verify-full",
			},
			want: "postgres://admin:pass@rds.example.com:5432/flint?sslmode=verify-full",
		},
		{
			name: "default port",
			cfg: dbkit.Config{
				Host:     "localhost",
				Database: "test",
				User:     "user",
				Password: "pw",
			},
			want: "postgres://user:pw@localhost:5432/test?sslmode=disable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.DSN()
			if got != tt.want {
				t.Errorf("DSN() = %q, want %q", got, tt.want)
			}
		})
	}
}
