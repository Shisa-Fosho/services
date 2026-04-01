package postgres

import (
	"testing"
	"time"
)

func TestDefaultPoolConfig(t *testing.T) {
	t.Parallel()

	cfg := DefaultPoolConfig("postgres://localhost:5432/testdb")

	if cfg.DSN != "postgres://localhost:5432/testdb" {
		t.Errorf("DSN = %v, want postgres://localhost:5432/testdb", cfg.DSN)
	}
	if cfg.MaxConns != 10 {
		t.Errorf("MaxConns = %v, want 10", cfg.MaxConns)
	}
	if cfg.MinConns != 2 {
		t.Errorf("MinConns = %v, want 2", cfg.MinConns)
	}
	if cfg.MaxConnLifetime != time.Hour {
		t.Errorf("MaxConnLifetime = %v, want 1h", cfg.MaxConnLifetime)
	}
	if cfg.MaxConnIdleTime != 30*time.Minute {
		t.Errorf("MaxConnIdleTime = %v, want 30m", cfg.MaxConnIdleTime)
	}
	if cfg.HealthCheckPeriod != time.Minute {
		t.Errorf("HealthCheckPeriod = %v, want 1m", cfg.HealthCheckPeriod)
	}
}

func TestNewPool_EmptyDSN(t *testing.T) {
	t.Parallel()

	_, err := NewPool(t.Context(), PoolConfig{})
	if err == nil {
		t.Fatal("expected error for empty DSN, got nil")
	}
}

func TestNewPool_InvalidDSN(t *testing.T) {
	t.Parallel()

	_, err := NewPool(t.Context(), PoolConfig{DSN: "not-a-valid-dsn"})
	if err == nil {
		t.Fatal("expected error for invalid DSN, got nil")
	}
}
