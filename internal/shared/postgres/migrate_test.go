package postgres

import (
	"testing"
)

func TestRunMigrations_EmptyPath(t *testing.T) {
	t.Parallel()

	err := RunMigrations(MigrateConfig{
		DSN:            "postgres://localhost:5432/testdb",
		MigrationsPath: "",
	})
	if err == nil {
		t.Fatal("expected error for empty migrations path, got nil")
	}
}

func TestRollbackLast_EmptyPath(t *testing.T) {
	t.Parallel()

	err := RollbackLast(MigrateConfig{
		DSN:            "postgres://localhost:5432/testdb",
		MigrationsPath: "",
	})
	if err == nil {
		t.Fatal("expected error for empty migrations path, got nil")
	}
}
