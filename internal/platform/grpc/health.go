package grpc

import (
	"context"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// HealthChecker reports whether a dependency is healthy.
type HealthChecker interface {
	Check(ctx context.Context) error
}

// PoolHealthChecker verifies database connectivity via pgxpool.Pool.Ping.
// It accepts any type with a Ping method to avoid a direct pgx dependency.
type PoolHealthChecker struct {
	pool interface {
		Ping(ctx context.Context) error
	}
}

// NewPoolHealthChecker creates a health checker for a pgx connection pool.
func NewPoolHealthChecker(pool interface {
	Ping(ctx context.Context) error
}) *PoolHealthChecker {
	return &PoolHealthChecker{pool: pool}
}

// Check pings the database to verify connectivity.
func (c *PoolHealthChecker) Check(ctx context.Context) error {
	return c.pool.Ping(ctx)
}

// WatchHealth periodically checks the given HealthChecker and updates the
// gRPC health server status. It blocks until ctx is cancelled.
func WatchHealth(ctx context.Context, hs *health.Server, serviceName string, checker HealthChecker, interval time.Duration, logger *zap.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := checker.Check(checkCtx)
			cancel()

			if err != nil {
				logger.Warn("health check failed", zap.Error(err))
				hs.SetServingStatus(serviceName, healthpb.HealthCheckResponse_NOT_SERVING)
			} else {
				hs.SetServingStatus(serviceName, healthpb.HealthCheckResponse_SERVING)
			}
		}
	}
}
