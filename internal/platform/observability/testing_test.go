package observability

import (
	"testing"

	"go.uber.org/zap"
)

func TestNewNopLogger(t *testing.T) {
	t.Parallel()

	logger := NewNopLogger()
	if logger == nil {
		t.Fatal("NewNopLogger returned nil")
	}

	// Should not panic.
	logger.Info("discarded", zap.String("key", "value"))
}

func TestNewTestLogger(t *testing.T) {
	t.Parallel()

	logger := NewTestLogger(t)
	if logger == nil {
		t.Fatal("NewTestLogger returned nil")
	}

	// Should write to test log output.
	logger.Info("test log entry", zap.String("test", "value"))
}
