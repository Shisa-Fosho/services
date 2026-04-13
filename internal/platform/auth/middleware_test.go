package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthenticate_ValidToken(t *testing.T) {
	t.Parallel()

	jwtMgr, _ := NewJWTManager(testJWTConfig())
	addr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	token, _ := jwtMgr.IssueAccessToken(addr)

	var gotAddr string
	handler := Authenticate(jwtMgr)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAddr = UserAddressFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if gotAddr != addr {
		t.Errorf("address = %q, want %q", gotAddr, addr)
	}
}

func TestAuthenticate_MissingHeader(t *testing.T) {
	t.Parallel()

	jwtMgr, _ := NewJWTManager(testJWTConfig())
	handler := Authenticate(jwtMgr)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthenticate_MalformedHeader(t *testing.T) {
	t.Parallel()

	jwtMgr, _ := NewJWTManager(testJWTConfig())
	handler := Authenticate(jwtMgr)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "NotBearer some-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestAuthenticate_ExpiredToken(t *testing.T) {
	t.Parallel()

	jwtMgr, _ := NewJWTManager(testJWTConfig())
	handler := Authenticate(jwtMgr)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("Authorization", "Bearer expired.jwt.token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(w.Body.String(), "invalid token") {
		t.Errorf("body = %q, want 'invalid token'", w.Body.String())
	}
}
