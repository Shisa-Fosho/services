package observability

import (
	"testing"

	"go.uber.org/zap"
)

func TestNewLogger(t *testing.T) {
	t.Parallel()

	logger, err := NewLogger("test-service")
	if err != nil {
		t.Fatalf("NewLogger returned error: %v", err)
	}
	if logger == nil {
		t.Fatal("NewLogger returned nil")
	}

	// Verify logger can write without panic.
	logger.Info("test message", zap.String("key", "value"))
}
