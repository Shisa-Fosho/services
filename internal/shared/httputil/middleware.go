package httputil

import (
	"net/http"
	"runtime/debug"
	"time"

	"go.uber.org/zap"

	"github.com/Shisa-Fosho/services/internal/shared/observability"
)

// RequestID is HTTP middleware that extracts or generates a unique request ID.
// It checks the X-Request-ID header first; if absent, generates one via
// observability.NewRequestID. The ID is stored in the request context and
// set on the response header.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = observability.NewRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		ctx := observability.WithRequestID(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// StatusWriter wraps http.ResponseWriter to capture the status code for
// middleware that observes response outcomes (logging, audit trails, etc.).
// Zero value is usable with Code defaulting to 0 until the handler writes;
// use NewStatusWriter to get the 200-default wrapper.
type StatusWriter struct {
	http.ResponseWriter
	Code    int
	written bool
}

// NewStatusWriter wraps w and defaults Code to 200 (matching stdlib behavior
// when a handler writes a body without calling WriteHeader).
func NewStatusWriter(w http.ResponseWriter) *StatusWriter {
	return &StatusWriter{ResponseWriter: w, Code: http.StatusOK}
}

// WriteHeader captures the status code on the first call and forwards to
// the wrapped ResponseWriter.
func (writer *StatusWriter) WriteHeader(code int) {
	if !writer.written {
		writer.Code = code
		writer.written = true
	}
	writer.ResponseWriter.WriteHeader(code)
}

// Write forwards to the wrapped ResponseWriter, defaulting Code to 200 if
// the handler wrote a body without first calling WriteHeader.
func (writer *StatusWriter) Write(data []byte) (int, error) {
	if !writer.written {
		writer.Code = http.StatusOK
		writer.written = true
	}
	return writer.ResponseWriter.Write(data)
}

// Logging returns HTTP middleware that logs each request with method, path,
// status code, duration, and request ID.
func Logging(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			writer := NewStatusWriter(w)

			next.ServeHTTP(writer, r)

			logger.Info("http request",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", writer.Code),
				zap.Duration("duration", time.Since(start)),
				zap.String("request_id", observability.RequestIDFrom(r.Context())),
			)
		})
	}
}

// Recovery returns HTTP middleware that recovers from panics, logs the stack
// trace, and returns a 500 Internal Server Error response.
func Recovery(logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered",
						zap.Any("panic", rec),
						zap.String("stack", string(debug.Stack())),
						zap.String("request_id", observability.RequestIDFrom(r.Context())),
					)
					ErrorResponse(w, http.StatusInternalServerError, "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
