package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeAdminRepo is a tiny in-memory data.AdminRepository for middleware tests.
type fakeAdminRepo struct {
	admins map[string]bool
	err    error
}

func (f *fakeAdminRepo) IsAdminWallet(_ context.Context, address string) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.admins[address], nil
}

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

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusOK)
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

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestAuthenticate_MalformedHeader(t *testing.T) {
	t.Parallel()

	jwtMgr, _ := NewJWTManager(testJWTConfig())
	handler := Authenticate(jwtMgr)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "NotBearer some-token")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestAuthenticate_ExpiredToken(t *testing.T) {
	t.Parallel()

	jwtMgr, _ := NewJWTManager(testJWTConfig())
	handler := Authenticate(jwtMgr)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	request := httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Authorization", "Bearer expired.jwt.token")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(recorder.Body.String(), "invalid token") {
		t.Errorf("body = %q, want 'invalid token'", recorder.Body.String())
	}
}

// requireAdminTestHandler wraps RequireAdmin around a sentinel handler that
// records whether it was called and what admin address made it through.
func requireAdminTestHandler(t *testing.T, repo *fakeAdminRepo) (http.Handler, *bool, *string) {
	t.Helper()
	called := false
	var gotAdmin string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		gotAdmin = AdminAddressFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})
	return RequireAdmin(repo)(next), &called, &gotAdmin
}

func TestRequireAdmin_AdminPassesThrough(t *testing.T) {
	t.Parallel()

	adminAddr := "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"
	repo := &fakeAdminRepo{admins: map[string]bool{adminAddr: true}}
	handler, called, gotAdmin := requireAdminTestHandler(t, repo)

	request := httptest.NewRequest(http.MethodGet, "/", nil).
		WithContext(WithUserAddress(context.Background(), adminAddr))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if !*called {
		t.Error("downstream handler was not called")
	}
	if *gotAdmin != adminAddr {
		t.Errorf("AdminAddressFrom = %q, want %q", *gotAdmin, adminAddr)
	}
}

func TestRequireAdmin_NormalizesAddressCase(t *testing.T) {
	t.Parallel()

	// Store lowercase in the "DB", but Authenticate will have placed an
	// uppercase/mixed-case form (as it came from the JWT sub claim).
	adminAddr := "0xabcdefabcdefabcdefabcdefabcdefabcdefabcd"
	repo := &fakeAdminRepo{admins: map[string]bool{adminAddr: true}}
	handler, called, gotAdmin := requireAdminTestHandler(t, repo)

	mixedCase := "0xABCDEFabcdefABCDEFabcdefABCDEFabcdefABCD"
	request := httptest.NewRequest(http.MethodGet, "/", nil).
		WithContext(WithUserAddress(context.Background(), mixedCase))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Errorf("status = %d, want %d (body %q)", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if !*called {
		t.Error("downstream handler was not called")
	}
	if *gotAdmin != adminAddr {
		t.Errorf("AdminAddressFrom = %q, want normalized %q", *gotAdmin, adminAddr)
	}
}

func TestRequireAdmin_NonAdminGets403(t *testing.T) {
	t.Parallel()

	repo := &fakeAdminRepo{admins: map[string]bool{
		"0xadmin00000000000000000000000000000000000": true,
	}}
	handler, called, _ := requireAdminTestHandler(t, repo)

	request := httptest.NewRequest(http.MethodGet, "/", nil).
		WithContext(WithUserAddress(context.Background(),
			"0xnotadmin00000000000000000000000000000000"))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	if *called {
		t.Error("downstream handler should not be called for non-admin")
	}
}

func TestRequireAdmin_MissingUserContextGets403(t *testing.T) {
	t.Parallel()

	repo := &fakeAdminRepo{admins: map[string]bool{}}
	handler, called, _ := requireAdminTestHandler(t, repo)

	// No WithUserAddress in context — simulates RequireAdmin being reached
	// without Authenticate running first.
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	if *called {
		t.Error("downstream handler should not be called without user context")
	}
}

func TestRequireAdmin_RepoErrorGets500(t *testing.T) {
	t.Parallel()

	repo := &fakeAdminRepo{err: errors.New("db down")}
	handler, called, _ := requireAdminTestHandler(t, repo)

	request := httptest.NewRequest(http.MethodGet, "/", nil).
		WithContext(WithUserAddress(context.Background(),
			"0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusInternalServerError)
	}
	if *called {
		t.Error("downstream handler should not be called on repo error")
	}
}
