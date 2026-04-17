package observability

import (
	"fmt"

	"go.uber.org/zap"
)

// NewLogger creates a new structured zap logger configured for production use.
// The logger includes a "service" field with the provided service name for
// consistent identification across log aggregation systems.
func NewLogger(serviceName string) (*zap.Logger, error) {
	logger, err := zap.NewProduction()
	if err != nil {
		return nil, fmt.Errorf("creating logger: %w", err)
	}

	return logger.With(zap.String("service", serviceName)), nil
}
