package postgres

import (
	"errors"
	"fmt"
	"os"

	"github.com/golang-migrate/migrate/v4"
	// pgx v5 database driver for golang-migrate.
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	// File source driver for golang-migrate.
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// MigrateConfig configures database migrations.
type MigrateConfig struct {
	DSN            string // PostgreSQL connection string.
	MigrationsPath string // Path to migration files (e.g., "migrations/trading").
}

// RunMigrations runs all pending migrations.
// Returns nil if no migrations are pending.
func RunMigrations(cfg MigrateConfig) error {
	m, err := newMigrate(cfg)
	if err != nil {
		return err
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("running migrations: %w", err)
	}

	return nil
}

// RollbackLast rolls back the most recent migration.
func RollbackLast(cfg MigrateConfig) error {
	m, err := newMigrate(cfg)
	if err != nil {
		return err
	}

	if err := m.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("rolling back migration: %w", err)
	}

	return nil
}

// MigrateFromEnv runs migrations using the DATABASE_URL environment variable.
func MigrateFromEnv(migrationsPath string) error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return fmt.Errorf("running migrations: DATABASE_URL is not set")
	}
	return RunMigrations(MigrateConfig{
		DSN:            dsn,
		MigrationsPath: migrationsPath,
	})
}

func newMigrate(cfg MigrateConfig) (*migrate.Migrate, error) {
	if cfg.MigrationsPath == "" {
		return nil, fmt.Errorf("creating migrator: migrations path is required")
	}

	source := "file://" + cfg.MigrationsPath

	m, err := migrate.New(source, cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("creating migrator: %w", err)
	}

	return m, nil
}
