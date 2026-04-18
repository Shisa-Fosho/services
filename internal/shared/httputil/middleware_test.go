package httputil

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap/zaptest"

	"github.com/Shisa-Fosho/services/internal/shared/observability"
)

func TestRequestID_GeneratesWhenMissing(t *testing.T) {
	t.Parallel()

	var gotID string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = observability.RequestIDFrom(r.Context())
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(w, r)

	if gotID == "" {
		t.Error("expected generated request ID in context, got empty")
	}
	if respID := w.Header().Get("X-Request-ID"); respID == "" {
		t.Error("expected X-Request-ID response header, got empty")
	}
	if w.Header().Get("X-Request-ID") != gotID {
		t.Errorf("response header = %q, context = %q, want match", w.Header().Get("X-Request-ID"), gotID)
	}
}

func TestRequestID_UsesExisting(t *testing.T) {
	t.Parallel()

	var gotID string
	handler := RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotID = observability.RequestIDFrom(r.Context())
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Request-ID", "my-custom-id")
	handler.ServeHTTP(w, r)

	if gotID != "my-custom-id" {
		t.Errorf("context request ID = %q, want %q", gotID, "my-custom-id")
	}
	if respID := w.Header().Get("X-Request-ID"); respID != "my-custom-id" {
		t.Errorf("response header = %q, want %q", respID, "my-custom-id")
	}
}

func TestLogging_LogsRequest(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	handler := Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/test", nil)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestLogging_CapturesStatusCode(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	handler := Logging(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/missing", nil)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestRecovery_HandlerPanic(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	handler := Recovery(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("something went wrong")
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/panic", nil)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(w.Body.String(), "internal server error") {
		t.Errorf("body = %q, want error message", w.Body.String())
	}
}

func TestRecovery_NormalHandler(t *testing.T) {
	t.Parallel()

	logger := zaptest.NewLogger(t)
	handler := Recovery(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/ok", nil)
	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}
