package dbkit

import "embed"

// Migrations contains the embedded SQL migration files.
//
//go:embed migrations/*.sql
var Migrations embed.FS
