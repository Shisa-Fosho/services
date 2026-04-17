package observability

import (
	"context"
	"testing"
)

func TestRequestIDContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	requestID := "req-123"

	ctx = WithRequestID(ctx, requestID)

	got := RequestIDFrom(ctx)
	if got != requestID {
		t.Errorf("RequestIDFrom() = %v, want %v", got, requestID)
	}
}

func TestRequestIDFromEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	got := RequestIDFrom(ctx)
	if got != "" {
		t.Errorf("RequestIDFrom() = %v, want empty string", got)
	}
}

func TestServiceNameContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	name := "trading"

	ctx = WithServiceName(ctx, name)

	got := ServiceNameFrom(ctx)
	if got != name {
		t.Errorf("ServiceNameFrom() = %v, want %v", got, name)
	}
}

func TestServiceNameFromEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	got := ServiceNameFrom(ctx)
	if got != "" {
		t.Errorf("ServiceNameFrom() = %v, want empty string", got)
	}
}

func TestNewRequestID(t *testing.T) {
	t.Parallel()

	id1 := NewRequestID()
	id2 := NewRequestID()

	if id1 == "" {
		t.Error("NewRequestID() returned empty string")
	}
	if id1 == id2 {
		t.Errorf("NewRequestID() returned duplicate IDs: %v", id1)
	}
}
