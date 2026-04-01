package observability

import (
	"context"
	"crypto/rand"
	"fmt"
)

type contextKey string

const (
	requestIDKey   contextKey = "request_id"
	serviceNameKey contextKey = "service_name"
)

// WithRequestID returns a new context with the given request ID attached.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey, requestID)
}

// RequestIDFrom retrieves the request ID from the context.
// Returns an empty string if no request ID is present.
func RequestIDFrom(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

// WithServiceName returns a new context with the given service name attached.
func WithServiceName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, serviceNameKey, name)
}

// ServiceNameFrom retrieves the service name from the context.
// Returns an empty string if no service name is present.
func ServiceNameFrom(ctx context.Context) string {
	if name, ok := ctx.Value(serviceNameKey).(string); ok {
		return name
	}
	return ""
}

// NewRequestID generates a new unique request ID using crypto/rand.
func NewRequestID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand.Read never returns an error on supported platforms,
		// but return a zero-filled ID rather than panicking if it somehow does.
		return "00000000-0000-0000-0000-000000000000"
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
