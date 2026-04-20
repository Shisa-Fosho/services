package grpc_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	sharedgrpc "github.com/Shisa-Fosho/services/internal/shared/grpc"
	"go.uber.org/zap"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// mockChecker is a test double for HealthChecker.
type mockChecker struct {
	mu  sync.Mutex
	err error
}

func (checker *mockChecker) Check(_ context.Context) error {
	checker.mu.Lock()
	defer checker.mu.Unlock()
	return checker.err
}

func (checker *mockChecker) setErr(err error) {
	checker.mu.Lock()
	defer checker.mu.Unlock()
	checker.err = err
}

func TestWatchHealth_SetsServing(t *testing.T) {
	t.Parallel()

	healthServer := health.NewServer()
	checker := &mockChecker{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sharedgrpc.WatchHealth(ctx, healthServer, "test", checker, 10*time.Millisecond, zap.NewNop())

	// Wait for at least one tick.
	time.Sleep(50 * time.Millisecond)

	resp, err := healthServer.Check(context.Background(), &healthpb.HealthCheckRequest{Service: "test"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if resp.Status != healthpb.HealthCheckResponse_SERVING {
		t.Errorf("status = %v, want SERVING", resp.Status)
	}
}

func TestWatchHealth_TransitionsToNotServing(t *testing.T) {
	t.Parallel()

	healthServer := health.NewServer()
	checker := &mockChecker{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sharedgrpc.WatchHealth(ctx, healthServer, "test", checker, 10*time.Millisecond, zap.NewNop())

	// Let it become healthy first.
	time.Sleep(50 * time.Millisecond)

	// Make it fail.
	checker.setErr(errors.New("db gone"))
	time.Sleep(50 * time.Millisecond)

	resp, err := healthServer.Check(context.Background(), &healthpb.HealthCheckRequest{Service: "test"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if resp.Status != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Errorf("status = %v, want NOT_SERVING", resp.Status)
	}
}

func TestWatchHealth_RecoversAfterFailure(t *testing.T) {
	t.Parallel()

	healthServer := health.NewServer()
	checker := &mockChecker{err: errors.New("initial failure")}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go sharedgrpc.WatchHealth(ctx, healthServer, "test", checker, 10*time.Millisecond, zap.NewNop())

	// Wait for NOT_SERVING.
	time.Sleep(50 * time.Millisecond)

	resp, err := healthServer.Check(context.Background(), &healthpb.HealthCheckRequest{Service: "test"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if resp.Status != healthpb.HealthCheckResponse_NOT_SERVING {
		t.Errorf("status = %v, want NOT_SERVING", resp.Status)
	}

	// Recover.
	checker.setErr(nil)
	time.Sleep(50 * time.Millisecond)

	resp, err = healthServer.Check(context.Background(), &healthpb.HealthCheckRequest{Service: "test"})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if resp.Status != healthpb.HealthCheckResponse_SERVING {
		t.Errorf("status = %v, want SERVING", resp.Status)
	}
}
