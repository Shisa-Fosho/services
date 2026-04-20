package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap/zaptest"

	"github.com/Shisa-Fosho/services/internal/platform/data"
	"github.com/Shisa-Fosho/services/internal/shared/eth"
)

// fakeVerifier implements auth.MessageVerifier for handler tests.
type fakeVerifier struct {
	address string
	err     error
}

func (verifier *fakeVerifier) Verify(message, signature string) (string, error) {
	if verifier.err != nil {
		return "", verifier.err
	}
	return verifier.address, nil
}

// fakeRepo is an in-memory SessionRepository implementation for tests. It
// only satisfies the subset of SessionRepository that the session handler
// actually calls — position methods are stubbed no-ops.
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

func (store *fakeRepo) CreateUser(_ context.Context, user *data.User) error {
	if _, exists := store.users[user.Address]; exists {
		return data.ErrDuplicateUser
	}
	store.users[user.Address] = user
	return nil
}

func (store *fakeRepo) GetUserByAddress(_ context.Context, address string) (*data.User, error) {
	user, ok := store.users[address]
	if !ok {
		return nil, data.ErrNotFound
	}
	return user, nil
}

func (store *fakeRepo) GetUserByEmail(_ context.Context, email string) (*data.User, error) {
	for _, user := range store.users {
		if user.Email != nil && *user.Email == email {
			return user, nil
		}
	}
	return nil, data.ErrNotFound
}

func (store *fakeRepo) UpsertPosition(_ context.Context, _ *data.Position) error { return nil }
func (store *fakeRepo) GetPositionsByUser(_ context.Context, _ string) ([]*data.Position, error) {
	return nil, nil
}
func (store *fakeRepo) GetPosition(_ context.Context, _ string, _ string, _ data.Side) (*data.Position, error) {
	return nil, data.ErrNotFound
}

func (store *fakeRepo) StoreRefreshToken(_ context.Context, token *data.RefreshToken) error {
	store.refreshTokens[token.ID] = token
	return nil
}

func (store *fakeRepo) GetRefreshToken(_ context.Context, id string) (*data.RefreshToken, error) {
	token, ok := store.refreshTokens[id]
	if !ok {
		return nil, data.ErrNotFound
	}
	return token, nil
}

func (store *fakeRepo) RevokeRefreshToken(_ context.Context, id string) error {
	token, ok := store.refreshTokens[id]
	if !ok {
		return data.ErrNotFound
	}
	token.Revoked = true
	return nil
}

func (store *fakeRepo) RevokeAllRefreshTokens(_ context.Context, userAddress string) error {
	for _, token := range store.refreshTokens {
		if token.UserAddress == userAddress {
			token.Revoked = true
		}
	}
	return nil
}

func testHandler(t *testing.T, repo *fakeRepo, verifier *fakeVerifier) *Handler {
	t.Helper()
	jwtMgr, err := NewJWTManager(testJWTConfig())
	if err != nil {
		t.Fatalf("creating JWT manager: %v", err)
	}
	safeCfg := eth.SafeConfig{
		FactoryAddress:   common.HexToAddress("0xa6B71E26C5e0845f74c812102Ca7114b6a896AB2"),
		SingletonAddress: common.HexToAddress("0xd9Db270c1B5E3Bd161E8c8503c55cEABeE709552"),
		FallbackHandler:  common.HexToAddress("0xf48f2B2d2a534e402487b3ee7C18c33Aec0Fe5e4"),
	}
	return NewHandler(zaptest.NewLogger(t), repo, jwtMgr, verifier, safeCfg, false)
}

func TestSignupWallet_Success(t *testing.T) {
	t.Parallel()

	addr := "0x1234567890AbcdEF1234567890aBcdef12345678"
	repo := newFakeRepo()
	verifier := &fakeVerifier{address: addr}
	handler := testHandler(t, repo, verifier)

	body := `{"message":"test","signature":"0xtest","username":"alice"}`
	request := httptest.NewRequest(http.MethodPost, "/auth/signup/wallet", strings.NewReader(body))
	recorder := httptest.NewRecorder()

	handler.signupWallet(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var resp authResponse
	json.NewDecoder(recorder.Body).Decode(&resp)
	if resp.AccessToken == "" {
		t.Error("expected non-empty access token")
	}
	if resp.Address != addr {
		t.Errorf("address = %q, want %q", resp.Address, addr)
	}
	if resp.SafeAddress == "" {
		t.Error("expected non-empty safe address")
	}

	cookies := recorder.Result().Cookies()
	found := false
	for _, cookie := range cookies {
		if cookie.Name == refreshCookieName {
			found = true
			if !cookie.HttpOnly {
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
	handler := testHandler(t, repo, verifier)

	body := `{"message":"test","signature":"0xtest","username":"bob"}`
	request := httptest.NewRequest(http.MethodPost, "/auth/signup/wallet", strings.NewReader(body))
	recorder := httptest.NewRecorder()

	handler.signupWallet(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusConflict)
	}
}

func TestSignupWallet_MissingUsername(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	verifier := &fakeVerifier{address: "0x1111111111111111111111111111111111111111"}
	handler := testHandler(t, repo, verifier)

	body := `{"message":"test","signature":"0xtest"}`
	request := httptest.NewRequest(http.MethodPost, "/auth/signup/wallet", strings.NewReader(body))
	recorder := httptest.NewRecorder()

	handler.signupWallet(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
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
	handler := testHandler(t, repo, verifier)

	body := `{"message":"test","signature":"0xtest"}`
	request := httptest.NewRequest(http.MethodPost, "/auth/login/wallet", strings.NewReader(body))
	recorder := httptest.NewRecorder()

	handler.loginWallet(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
}

func TestLoginWallet_UnknownUser(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	verifier := &fakeVerifier{address: "0x1111111111111111111111111111111111111111"}
	handler := testHandler(t, repo, verifier)

	body := `{"message":"test","signature":"0xtest"}`
	request := httptest.NewRequest(http.MethodPost, "/auth/login/wallet", strings.NewReader(body))
	recorder := httptest.NewRecorder()

	handler.loginWallet(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestRefresh_Success(t *testing.T) {
	t.Parallel()

	addr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	repo := newFakeRepo()
	jwtMgr, _ := NewJWTManager(testJWTConfig())

	tokenStr, jti, expiresAt, _ := jwtMgr.IssueRefreshToken(addr)
	repo.refreshTokens[jti] = &data.RefreshToken{
		ID:          jti,
		UserAddress: addr,
		ExpiresAt:   expiresAt,
	}

	verifier := &fakeVerifier{address: addr}
	handler := testHandler(t, repo, verifier)

	request := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	request.AddCookie(&http.Cookie{Name: refreshCookieName, Value: tokenStr})
	recorder := httptest.NewRecorder()

	handler.refresh(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
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
		Revoked:     true,
	}

	verifier := &fakeVerifier{address: addr}
	handler := testHandler(t, repo, verifier)

	request := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	request.AddCookie(&http.Cookie{Name: refreshCookieName, Value: tokenStr})
	recorder := httptest.NewRecorder()

	handler.refresh(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestRefresh_MissingCookie(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	verifier := &fakeVerifier{}
	handler := testHandler(t, repo, verifier)

	request := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	recorder := httptest.NewRecorder()

	handler.refresh(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestLogout_ClearsCookie(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	verifier := &fakeVerifier{}
	handler := testHandler(t, repo, verifier)

	request := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	recorder := httptest.NewRecorder()

	handler.logout(recorder, request)

	if recorder.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusNoContent)
	}

	cookies := recorder.Result().Cookies()
	for _, cookie := range cookies {
		if cookie.Name == refreshCookieName && cookie.MaxAge != -1 {
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
	handler := testHandler(t, repo, verifier)

	request := httptest.NewRequest(http.MethodGet, "/auth/session", nil)
	request = request.WithContext(WithUserAddress(request.Context(), addr))
	recorder := httptest.NewRecorder()

	handler.session(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var resp sessionResponse
	json.NewDecoder(recorder.Body).Decode(&resp)
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
	handler := testHandler(t, repo, verifier)

	mux := http.NewServeMux()
	handler.RegisterRoutes(mux, nil)

	request := httptest.NewRequest(http.MethodGet, "/auth/session", nil)
	request.Header.Set("Authorization", "Bearer invalid-token")
	recorder := httptest.NewRecorder()

	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestNonce_ReturnsNonce(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	verifier := &fakeVerifier{}
	handler := testHandler(t, repo, verifier)

	request := httptest.NewRequest(http.MethodGet, "/auth/nonce", nil)
	recorder := httptest.NewRecorder()

	handler.nonce(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	var resp nonceResponse
	json.NewDecoder(recorder.Body).Decode(&resp)
	if resp.Nonce == "" {
		t.Error("expected non-empty nonce")
	}
}
