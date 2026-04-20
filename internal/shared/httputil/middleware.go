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

// statusWriter wraps http.ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	code    int
	written bool
}

func (writer *statusWriter) WriteHeader(code int) {
	if !writer.written {
		writer.code = code
		writer.written = true
	}
	writer.ResponseWriter.WriteHeader(code)
}

func (writer *statusWriter) Write(data []byte) (int, error) {
	if !writer.written {
		writer.code = http.StatusOK
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
			writer := &statusWriter{ResponseWriter: w, code: http.StatusOK}

			next.ServeHTTP(writer, r)

			logger.Info("http request",
				zap.String("method", r.Method),
				zap.String("path", r.URL.Path),
				zap.Int("status", writer.code),
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
