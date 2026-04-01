package grpc

import (
	"github.com/Shisa-Fosho/services/internal/platform/observability"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
)

// NewServer creates a gRPC server pre-wired with recovery, logging, and metrics
// interceptors, OpenTelemetry tracing via stats handler, server reflection,
// and health service.
func NewServer(logger *zap.Logger, metrics *observability.Metrics, hs *health.Server) *grpc.Server {
	srv := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(
			RecoveryInterceptor(logger),
			LoggingInterceptor(logger),
			MetricsInterceptor(metrics),
		),
	)

	reflection.Register(srv)

	if hs != nil {
		healthpb.RegisterHealthServer(srv, hs)
	}

	return srv
}
