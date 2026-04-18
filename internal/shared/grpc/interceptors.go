package grpc

import (
	"context"
	"runtime/debug"
	"time"

	"github.com/Shisa-Fosho/services/internal/shared/observability"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// RecoveryInterceptor returns a unary server interceptor that recovers from
// panics in gRPC handlers. Instead of crashing the process, it logs the panic
// with a stack trace and returns a gRPC Internal error to the caller.
func RecoveryInterceptor(logger *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("panic recovered in gRPC handler",
					zap.Any("panic", r),
					zap.String("method", info.FullMethod),
					zap.String("stack", string(debug.Stack())),
				)
				err = status.Errorf(codes.Internal, "internal server error")
			}
		}()

		return handler(ctx, req)
	}
}

// LoggingInterceptor returns a unary server interceptor that logs every RPC
// call with its method name, duration, and resulting gRPC status code.
func LoggingInterceptor(logger *zap.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()

		resp, err := handler(ctx, req)

		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}

		logger.Info("gRPC call finished",
			zap.String("method", info.FullMethod),
			zap.Duration("duration", time.Since(start)),
			zap.String("code", code.String()),
		)

		return resp, err
	}
}

// MetricsInterceptor returns a unary server interceptor that records
// request count and duration into the provided Prometheus metrics.
func MetricsInterceptor(metrics *observability.Metrics) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()

		resp, err := handler(ctx, req)

		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}
		codeStr := code.String()

		metrics.RequestTotal.WithLabelValues(info.FullMethod, codeStr).Inc()
		metrics.RequestDuration.WithLabelValues(info.FullMethod, codeStr).Observe(time.Since(start).Seconds())

		return resp, err
	}
}
