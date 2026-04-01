package grpc_test

import (
	"context"
	"testing"

	platformgrpc "github.com/Shisa-Fosho/services/internal/platform/grpc"
	"github.com/Shisa-Fosho/services/internal/platform/observability"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeInfo returns a UnaryServerInfo for testing.
func fakeInfo(method string) *grpc.UnaryServerInfo {
	return &grpc.UnaryServerInfo{FullMethod: method}
}

func TestRecoveryInterceptor(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)

	tests := []struct {
		name     string
		handler  grpc.UnaryHandler
		wantCode codes.Code
		wantResp bool
	}{
		{
			name: "handler succeeds",
			handler: func(ctx context.Context, req any) (any, error) {
				return "ok", nil
			},
			wantCode: codes.OK,
			wantResp: true,
		},
		{
			name: "handler returns error",
			handler: func(ctx context.Context, req any) (any, error) {
				return nil, status.Errorf(codes.NotFound, "not found")
			},
			wantCode: codes.NotFound,
			wantResp: false,
		},
		{
			name: "handler panics",
			handler: func(ctx context.Context, req any) (any, error) {
				panic("something went wrong")
			},
			wantCode: codes.Internal,
			wantResp: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			interceptor := platformgrpc.RecoveryInterceptor(logger)
			resp, err := interceptor(context.Background(), nil, fakeInfo("/test.Service/Method"), tt.handler)

			if tt.wantResp && resp == nil {
				t.Error("expected non-nil response, got nil")
			}
			if !tt.wantResp && resp != nil {
				t.Errorf("expected nil response, got %v", resp)
			}

			gotCode := status.Code(err)
			if gotCode != tt.wantCode {
				t.Errorf("got code %v, want %v", gotCode, tt.wantCode)
			}
		})
	}
}

func TestLoggingInterceptor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		handler  grpc.UnaryHandler
		wantCode codes.Code
		wantResp any
	}{
		{
			name: "successful call",
			handler: func(ctx context.Context, req any) (any, error) {
				return "result", nil
			},
			wantCode: codes.OK,
			wantResp: "result",
		},
		{
			name: "failed call",
			handler: func(ctx context.Context, req any) (any, error) {
				return nil, status.Errorf(codes.InvalidArgument, "bad request")
			},
			wantCode: codes.InvalidArgument,
			wantResp: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			interceptor := platformgrpc.LoggingInterceptor(zap.NewNop())
			resp, err := interceptor(context.Background(), nil, fakeInfo("/test.Service/Method"), tt.handler)

			gotCode := status.Code(err)
			if gotCode != tt.wantCode {
				t.Errorf("got code %v, want %v", gotCode, tt.wantCode)
			}

			if resp != tt.wantResp {
				t.Errorf("got resp %v, want %v", resp, tt.wantResp)
			}
		})
	}
}

func TestMetricsInterceptor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		handler   grpc.UnaryHandler
		wantCode  string
		wantCount float64
	}{
		{
			name: "successful call increments counter",
			handler: func(ctx context.Context, req any) (any, error) {
				return "ok", nil
			},
			wantCode:  "OK",
			wantCount: 1,
		},
		{
			name: "failed call increments counter with error code",
			handler: func(ctx context.Context, req any) (any, error) {
				return nil, status.Errorf(codes.NotFound, "not found")
			},
			wantCode:  "NotFound",
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			metrics := observability.NewTestMetrics()
			interceptor := platformgrpc.MetricsInterceptor(metrics)

			_, _ = interceptor(context.Background(), nil, fakeInfo("/test.Service/Method"), tt.handler)

			counter := metrics.RequestTotal.WithLabelValues("/test.Service/Method", tt.wantCode)
			if got := testutil.ToFloat64(counter); got != tt.wantCount {
				t.Errorf("counter = %v, want %v", got, tt.wantCount)
			}
		})
	}
}
