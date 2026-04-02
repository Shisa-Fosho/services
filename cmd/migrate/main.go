package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Shisa-Fosho/services/internal/platform/postgres"
)

const usage = "usage: migrate <up|down>"

// migrationDirs lists migration directories in execution order.
// shared runs first (extensions, common schemas), then service-specific.
var migrationDirs = []string{
	"migrations/shared",
	"migrations/trading",
	"migrations/platform",
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, usage)
		os.Exit(1)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		fmt.Fprintln(os.Stderr, "DATABASE_URL is not set")
		os.Exit(1)
	}

	// golang-migrate pgx5 driver requires pgx5:// scheme.
	migrateDSN := strings.Replace(dsn, "postgres://", "pgx5://", 1)

	switch os.Args[1] {
	case "up":
		for _, dir := range migrationDirs {
			if !hasMigrations(dir) {
				continue
			}
			if err := postgres.RunMigrations(postgres.MigrateConfig{
				DSN:            dsnWithTable(migrateDSN, dir),
				MigrationsPath: dir,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "migrating %s: %v\n", dir, err)
				os.Exit(1)
			}
		}
	case "down":
		for i := len(migrationDirs) - 1; i >= 0; i-- {
			dir := migrationDirs[i]
			if !hasMigrations(dir) {
				continue
			}
			if err := postgres.RollbackLast(postgres.MigrateConfig{
				DSN:            dsnWithTable(migrateDSN, dir),
				MigrationsPath: dir,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "rolling back %s: %v\n", dir, err)
				os.Exit(1)
			}
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n%s\n", os.Args[1], usage)
		os.Exit(1)
	}
}

// hasMigrations returns true if the directory contains any .sql files.
func hasMigrations(dir string) bool {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.sql"))
	return len(matches) > 0
}

// dsnWithTable appends a per-directory migrations table name to the DSN.
// Each directory gets its own table (e.g., schema_migrations_shared) so
// migration versions don't collide across directories.
func dsnWithTable(dsn, dir string) string {
	name := filepath.Base(dir)
	sep := "&"
	if !strings.Contains(dsn, "?") {
		sep = "?"
	}
	return dsn + sep + "x-migrations-table=schema_migrations_" + name
}
