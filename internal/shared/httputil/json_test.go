package httputil

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSON(t *testing.T) {
	t.Parallel()

	type payload struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name: "valid body",
			body: `{"name":"alice","age":30}`,
		},
		{
			name:    "empty body",
			body:    "",
			wantErr: "request body is empty",
		},
		{
			name:    "invalid JSON",
			body:    `{bad json`,
			wantErr: "invalid JSON",
		},
		{
			name:    "unknown fields",
			body:    `{"name":"alice","unknown":"field"}`,
			wantErr: "invalid JSON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			var dst payload
			err := DecodeJSON(r, &dst)

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error = %q, want substring %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if dst.Name != "alice" {
				t.Errorf("Name = %q, want %q", dst.Name, "alice")
			}
			if dst.Age != 30 {
				t.Errorf("Age = %d, want %d", dst.Age, 30)
			}
		})
	}
}

func TestDecodeJSON_OversizedBody(t *testing.T) {
	t.Parallel()
	// Create a body larger than maxBodySize (1 MB).
	big := strings.Repeat("x", maxBodySize+1)
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"name":"`+big+`"}`))
	var dst struct{ Name string }
	err := DecodeJSON(r, &dst)
	if err == nil {
		t.Fatal("expected error for oversized body, got nil")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error = %q, want substring %q", err.Error(), "too large")
	}
}

func TestEncodeJSON(t *testing.T) {
	t.Parallel()

	type resp struct {
		Status string `json:"status"`
	}

	w := httptest.NewRecorder()
	err := EncodeJSON(w, http.StatusCreated, resp{Status: "ok"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.Code != http.StatusCreated {
		t.Errorf("status = %d, want %d", w.Code, http.StatusCreated)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
	if !strings.Contains(w.Body.String(), `"status":"ok"`) {
		t.Errorf("body = %q, want JSON containing status:ok", w.Body.String())
	}
}

func TestErrorResponse(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	ErrorResponse(w, http.StatusBadRequest, "something broke")

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !strings.Contains(w.Body.String(), `"error":"something broke"`) {
		t.Errorf("body = %q, want error envelope", w.Body.String())
	}
}
