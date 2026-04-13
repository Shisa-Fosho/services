package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap/zaptest"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Shisa-Fosho/services/internal/data"
	"github.com/Shisa-Fosho/services/internal/platform/eth"
)

// fakeVerifier implements MessageVerifier for handler tests.
type fakeVerifier struct {
	address string
	err     error
}

func (f *fakeVerifier) Verify(message, signature string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.address, nil
}

// fakeRepo implements data.Repository for handler tests.
type fakeRepo struct {
	users         map[string]*data.User
	refreshTokens map[string]*data.RefreshToken
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		users:         make(map[string]*data.User),
		refreshTokens: make(map[string]*data.RefreshToken),
	}
}

func (r *fakeRepo) CreateUser(_ context.Context, user *data.User) error {
	if _, exists := r.users[user.Address]; exists {
		return data.ErrDuplicateUser
	}
	r.users[user.Address] = user
	return nil
}

func (r *fakeRepo) GetUserByAddress(_ context.Context, address string) (*data.User, error) {
	u, ok := r.users[address]
	if !ok {
		return nil, data.ErrNotFound
	}
	return u, nil
}

func (r *fakeRepo) GetUserByEmail(_ context.Context, email string) (*data.User, error) {
	for _, u := range r.users {
		if u.Email != nil && *u.Email == email {
			return u, nil
		}
	}
	return nil, data.ErrNotFound
}

func (r *fakeRepo) UpsertPosition(_ context.Context, _ *data.Position) error { return nil }
func (r *fakeRepo) GetPositionsByUser(_ context.Context, _ string) ([]*data.Position, error) {
	return nil, nil
}
func (r *fakeRepo) GetPosition(_ context.Context, _ string, _ string, _ data.Side) (*data.Position, error) {
	return nil, data.ErrNotFound
}

func (r *fakeRepo) StoreRefreshToken(_ context.Context, token *data.RefreshToken) error {
	r.refreshTokens[token.ID] = token
	return nil
}

func (r *fakeRepo) GetRefreshToken(_ context.Context, id string) (*data.RefreshToken, error) {
	t, ok := r.refreshTokens[id]
	if !ok {
		return nil, data.ErrNotFound
	}
	return t, nil
}

func (r *fakeRepo) RevokeRefreshToken(_ context.Context, id string) error {
	t, ok := r.refreshTokens[id]
	if !ok {
		return data.ErrNotFound
	}
	t.Revoked = true
	return nil
}

func (r *fakeRepo) RevokeAllRefreshTokens(_ context.Context, userAddress string) error {
	for _, t := range r.refreshTokens {
		if t.UserAddress == userAddress {
			t.Revoked = true
		}
	}
	return nil
}

func testHandler(t *testing.T, repo *fakeRepo, verifier *fakeVerifier) *Handler {
	t.Helper()
	logger := zaptest.NewLogger(t)
	jwtMgr, err := NewJWTManager(testJWTConfig())
	if err != nil {
		t.Fatalf("creating JWT manager: %v", err)
	}
	safeCfg := eth.SafeConfig{
		FactoryAddress:   common.HexToAddress("0xa6B71E26C5e0845f74c812102Ca7114b6a896AB2"),
		SingletonAddress: common.HexToAddress("0xd9Db270c1B5E3Bd161E8c8503c55cEABeE709552"),
		FallbackHandler:  common.HexToAddress("0xf48f2B2d2a534e402487b3ee7C18c33Aec0Fe5e4"),
	}
	return NewHandler(logger, repo, jwtMgr, verifier, safeCfg, false)
}

func TestSignupWallet_Success(t *testing.T) {
	t.Parallel()

	addr := "0x1234567890AbcdEF1234567890aBcdef12345678"
	repo := newFakeRepo()
	verifier := &fakeVerifier{address: addr}
	h := testHandler(t, repo, verifier)

	body := `{"message":"test","signature":"0xtest","username":"alice"}`
	r := httptest.NewRequest(http.MethodPost, "/auth/signup/wallet", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.signupWallet(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp authResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.AccessToken == "" {
		t.Error("expected non-empty access token")
	}
	if resp.Address != addr {
		t.Errorf("address = %q, want %q", resp.Address, addr)
	}
	if resp.SafeAddress == "" {
		t.Error("expected non-empty safe address")
	}

	// Verify refresh cookie was set.
	cookies := w.Result().Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == refreshCookieName {
			found = true
			if !c.HttpOnly {
				t.Error("refresh cookie should be HttpOnly")
			}
		}
	}
	if !found {
		t.Error("expected refresh_token cookie")
	}
}

func TestSignupWallet_DuplicateUser(t *testing.T) {
	t.Parallel()

	addr := "0x1234567890AbcdEF1234567890aBcdef12345678"
	repo := newFakeRepo()
	repo.users[addr] = &data.User{Address: addr, Username: "alice"}
	verifier := &fakeVerifier{address: addr}
	h := testHandler(t, repo, verifier)

	body := `{"message":"test","signature":"0xtest","username":"bob"}`
	r := httptest.NewRequest(http.MethodPost, "/auth/signup/wallet", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.signupWallet(w, r)

	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", w.Code, http.StatusConflict)
	}
}

func TestSignupWallet_MissingUsername(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	verifier := &fakeVerifier{address: "0x1111111111111111111111111111111111111111"}
	h := testHandler(t, repo, verifier)

	body := `{"message":"test","signature":"0xtest"}`
	r := httptest.NewRequest(http.MethodPost, "/auth/signup/wallet", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.signupWallet(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestLoginWallet_Success(t *testing.T) {
	t.Parallel()

	addr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	repo := newFakeRepo()
	repo.users[addr] = &data.User{
		Address:     addr,
		Username:    "alice",
		SafeAddress: "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	verifier := &fakeVerifier{address: addr}
	h := testHandler(t, repo, verifier)

	body := `{"message":"test","signature":"0xtest"}`
	r := httptest.NewRequest(http.MethodPost, "/auth/login/wallet", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.loginWallet(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestLoginWallet_UnknownUser(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	verifier := &fakeVerifier{address: "0x1111111111111111111111111111111111111111"}
	h := testHandler(t, repo, verifier)

	body := `{"message":"test","signature":"0xtest"}`
	r := httptest.NewRequest(http.MethodPost, "/auth/login/wallet", strings.NewReader(body))
	w := httptest.NewRecorder()

	h.loginWallet(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestRefresh_Success(t *testing.T) {
	t.Parallel()

	addr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	repo := newFakeRepo()
	jwtMgr, _ := NewJWTManager(testJWTConfig())

	// Issue an initial refresh token and store it.
	tokenStr, jti, expiresAt, _ := jwtMgr.IssueRefreshToken(addr)
	repo.refreshTokens[jti] = &data.RefreshToken{
		ID:          jti,
		UserAddress: addr,
		ExpiresAt:   expiresAt,
	}

	verifier := &fakeVerifier{address: addr}
	h := testHandler(t, repo, verifier)

	r := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	r.AddCookie(&http.Cookie{Name: refreshCookieName, Value: tokenStr})
	w := httptest.NewRecorder()

	h.refresh(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Old token should be revoked.
	if !repo.refreshTokens[jti].Revoked {
		t.Error("old refresh token should be revoked after rotation")
	}
}

func TestRefresh_RevokedToken(t *testing.T) {
	t.Parallel()

	addr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	repo := newFakeRepo()
	jwtMgr, _ := NewJWTManager(testJWTConfig())

	tokenStr, jti, expiresAt, _ := jwtMgr.IssueRefreshToken(addr)
	repo.refreshTokens[jti] = &data.RefreshToken{
		ID:          jti,
		UserAddress: addr,
		ExpiresAt:   expiresAt,
		Revoked:     true, // Already revoked.
	}

	verifier := &fakeVerifier{address: addr}
	h := testHandler(t, repo, verifier)

	r := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	r.AddCookie(&http.Cookie{Name: refreshCookieName, Value: tokenStr})
	w := httptest.NewRecorder()

	h.refresh(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestRefresh_MissingCookie(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	verifier := &fakeVerifier{}
	h := testHandler(t, repo, verifier)

	r := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	w := httptest.NewRecorder()

	h.refresh(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestLogout_ClearsCookie(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	verifier := &fakeVerifier{}
	h := testHandler(t, repo, verifier)

	r := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	w := httptest.NewRecorder()

	h.logout(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNoContent)
	}

	cookies := w.Result().Cookies()
	for _, c := range cookies {
		if c.Name == refreshCookieName && c.MaxAge != -1 {
			t.Error("refresh cookie should have MaxAge -1")
		}
	}
}

func TestSession_Valid(t *testing.T) {
	t.Parallel()

	addr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	repo := newFakeRepo()
	repo.users[addr] = &data.User{
		Address:     addr,
		Username:    "alice",
		SafeAddress: "0xbbbb",
	}

	verifier := &fakeVerifier{}
	h := testHandler(t, repo, verifier)

	// session handler reads address from context (set by Authenticate middleware).
	r := httptest.NewRequest(http.MethodGet, "/auth/session", nil)
	r = r.WithContext(WithUserAddress(r.Context(), addr))
	w := httptest.NewRecorder()

	h.session(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp sessionResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Address != addr {
		t.Errorf("address = %q, want %q", resp.Address, addr)
	}
	if resp.Username != "alice" {
		t.Errorf("username = %q, want %q", resp.Username, "alice")
	}
}

func TestSession_InvalidToken(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	verifier := &fakeVerifier{}
	h := testHandler(t, repo, verifier)

	// Test through the registered route so the Authenticate middleware runs.
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	r := httptest.NewRequest(http.MethodGet, "/auth/session", nil)
	r.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestNonce_ReturnsNonce(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	verifier := &fakeVerifier{}
	h := testHandler(t, repo, verifier)

	r := httptest.NewRequest(http.MethodGet, "/auth/nonce", nil)
	w := httptest.NewRecorder()

	h.nonce(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var resp nonceResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Nonce == "" {
		t.Error("expected non-empty nonce")
	}
}
