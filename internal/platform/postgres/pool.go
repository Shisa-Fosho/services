// Package postgres provides PostgreSQL connection pool and migration utilities.
package postgres

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PoolConfig configures the pgx connection pool.
type PoolConfig struct {
	DSN               string
	MaxConns          int32         // Default: 10
	MinConns          int32         // Default: 2
	MaxConnLifetime   time.Duration // Default: 1h
	MaxConnIdleTime   time.Duration // Default: 30m
	HealthCheckPeriod time.Duration // Default: 1m
}

// DefaultPoolConfig returns a PoolConfig with sane defaults for the given DSN.
func DefaultPoolConfig(dsn string) PoolConfig {
	return PoolConfig{
		DSN:               dsn,
		MaxConns:          10,
		MinConns:          2,
		MaxConnLifetime:   time.Hour,
		MaxConnIdleTime:   30 * time.Minute,
		HealthCheckPeriod: time.Minute,
	}
}

// NewPool creates a pgx connection pool with the given configuration.
// It pings the database to verify connectivity before returning.
func NewPool(ctx context.Context, cfg PoolConfig) (*pgxpool.Pool, error) {
	if cfg.DSN == "" {
		return nil, fmt.Errorf("creating pool: DSN is required")
	}

	poolCfg, err := pgxpool.ParseConfig(cfg.DSN)
	if err != nil {
		return nil, fmt.Errorf("parsing DSN: %w", err)
	}

	if cfg.MaxConns > 0 {
		poolCfg.MaxConns = cfg.MaxConns
	}
	if cfg.MinConns > 0 {
		poolCfg.MinConns = cfg.MinConns
	}
	if cfg.MaxConnLifetime > 0 {
		poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	}
	if cfg.MaxConnIdleTime > 0 {
		poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	}
	if cfg.HealthCheckPeriod > 0 {
		poolCfg.HealthCheckPeriod = cfg.HealthCheckPeriod
	}

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("creating pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pinging database: %w", err)
	}

	return pool, nil
}

// PoolFromEnv creates a connection pool from the DATABASE_URL environment variable.
func PoolFromEnv(ctx context.Context) (*pgxpool.Pool, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, fmt.Errorf("creating pool: DATABASE_URL is not set")
	}
	return NewPool(ctx, DefaultPoolConfig(dsn))
}
