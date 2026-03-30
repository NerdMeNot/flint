package dbkit

import (
	"database/sql"
	"embed"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib" // pgx driver for database/sql
	"github.com/pressly/goose/v3"
)

// RunMigrations runs all pending SQL migrations from the embedded filesystem.
// The migrations FS should contain files like 001_initial.sql with goose annotations.
func RunMigrations(dsn string, migrations embed.FS, dir string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("dbkit: failed to open db for migration: %w", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrations)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("dbkit: failed to set goose dialect: %w", err)
	}

	if err := goose.Up(db, dir); err != nil {
		return fmt.Errorf("dbkit: migration failed: %w", err)
	}

	return nil
}

// RollbackMigration rolls back the most recent migration.
func RollbackMigration(dsn string, migrations embed.FS, dir string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("dbkit: failed to open db for rollback: %w", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrations)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("dbkit: failed to set goose dialect: %w", err)
	}

	if err := goose.Down(db, dir); err != nil {
		return fmt.Errorf("dbkit: rollback failed: %w", err)
	}

	return nil
}

// MigrationStatus returns the current migration version.
func MigrationStatus(dsn string, migrations embed.FS, dir string) error {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("dbkit: failed to open db for status: %w", err)
	}
	defer db.Close()

	goose.SetBaseFS(migrations)

	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("dbkit: failed to set goose dialect: %w", err)
	}

	return goose.Status(db, dir)
}
