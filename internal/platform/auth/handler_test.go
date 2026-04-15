package auth

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap/zaptest"

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
	apiKeys       map[string]*data.APIKey
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		users:         make(map[string]*data.User),
		refreshTokens: make(map[string]*data.RefreshToken),
		apiKeys:       make(map[string]*data.APIKey),
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

func (r *fakeRepo) GetAPIKeyByHash(_ context.Context, keyHash string) (*data.APIKey, error) {
	k, ok := r.apiKeys[keyHash]
	if !ok || k.Revoked || k.ExpiresAt.Before(time.Now()) {
		return nil, data.ErrNotFound
	}
	return k, nil
}

func (r *fakeRepo) UpsertAPIKey(_ context.Context, key *data.APIKey) error {
	r.apiKeys[key.KeyHash] = key
	return nil
}

func (r *fakeRepo) GetAPIKeysByUser(_ context.Context, userAddress string) ([]*data.APIKey, error) {
	var result []*data.APIKey
	for _, k := range r.apiKeys {
		if k.UserAddress == userAddress && !k.Revoked {
			result = append(result, k)
		}
	}
	return result, nil
}

func (r *fakeRepo) RevokeAPIKey(_ context.Context, keyHash string, userAddress string) error {
	k, ok := r.apiKeys[keyHash]
	if !ok || k.UserAddress != userAddress || k.Revoked {
		return data.ErrNotFound
	}
	k.Revoked = true
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
	apiKeyCfg := APIKeyConfig{
		DerivationSecret: []byte("test-derivation-secret-32-bytes!"),
		EncryptionKey:    []byte("test-encryption-key-32-bytes!!xx"),
		ChainID:          137,
	}
	return NewHandler(logger, repo, jwtMgr, verifier, safeCfg, false, apiKeyCfg)
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

// ---------------------------------------------------------------------------
// POST /auth/derive-api-key
// ---------------------------------------------------------------------------

// signedEIP712 generates a real EIP-712 ClobAuth signature using a freshly
// generated private key. Returns the canonical address and hex-encoded sig.
func signedEIP712(t *testing.T) (address string, sigHex string) {
	t.Helper()

	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	address = crypto.PubkeyToAddress(key.PublicKey).Hex()

	hash := eip712Hash(t, address)
	sig, err := crypto.Sign(hash, key)
	if err != nil {
		t.Fatalf("signing EIP-712 hash: %v", err)
	}

	return address, "0x" + hex.EncodeToString(sig)
}

func TestDeriveAPIKey_HappyPath(t *testing.T) {
	t.Parallel()

	address, sigHex := signedEIP712(t)

	repo := newFakeRepo()
	h := testHandler(t, repo, &fakeVerifier{})

	body := `{"signature":"` + sigHex + `","timestamp":"1700000000","nonce":"0"}`
	r := httptest.NewRequest(http.MethodPost, "/auth/derive-api-key", strings.NewReader(body))
	r = r.WithContext(WithUserAddress(r.Context(), address))
	w := httptest.NewRecorder()

	h.deriveAPIKey(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	var resp deriveAPIKeyResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.APIKey == "" {
		t.Error("expected non-empty api_key")
	}
	if resp.HMACSecret == "" {
		t.Error("expected non-empty hmac_secret")
	}
	if resp.ExpiresAt.IsZero() {
		t.Error("expected non-zero expires_at")
	}
	// Key should be persisted in the repo.
	if len(repo.apiKeys) != 1 {
		t.Errorf("repo apiKeys count = %d, want 1", len(repo.apiKeys))
	}
}

func TestDeriveAPIKey_MissingFields(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	h := testHandler(t, repo, &fakeVerifier{})

	// Provides nonce only — signature and timestamp are required.
	body := `{"nonce":"0"}`
	r := httptest.NewRequest(http.MethodPost, "/auth/derive-api-key", strings.NewReader(body))
	r = r.WithContext(WithUserAddress(r.Context(), "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"))
	w := httptest.NewRecorder()

	h.deriveAPIKey(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestDeriveAPIKey_InvalidSignature(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	h := testHandler(t, repo, &fakeVerifier{})

	addr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	// All-zero bytes: syntactically valid 65-byte hex but wrong signer.
	badSig := "0x" + hex.EncodeToString(make([]byte, 65))

	body := `{"signature":"` + badSig + `","timestamp":"1700000000","nonce":"0"}`
	r := httptest.NewRequest(http.MethodPost, "/auth/derive-api-key", strings.NewReader(body))
	r = r.WithContext(WithUserAddress(r.Context(), addr))
	w := httptest.NewRecorder()

	h.deriveAPIKey(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestDeriveAPIKey_Idempotent(t *testing.T) {
	t.Parallel()

	address, sigHex := signedEIP712(t)

	repo := newFakeRepo()
	h := testHandler(t, repo, &fakeVerifier{})

	body := `{"signature":"` + sigHex + `","timestamp":"1700000000","nonce":"0"}`

	// call issues the request and returns the decoded response.
	call := func() deriveAPIKeyResponse {
		rq := httptest.NewRequest(http.MethodPost, "/auth/derive-api-key", strings.NewReader(body))
		rq = rq.WithContext(WithUserAddress(rq.Context(), address))
		rw := httptest.NewRecorder()
		h.deriveAPIKey(rw, rq)
		if rw.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body: %s", rw.Code, http.StatusOK, rw.Body.String())
		}
		var resp deriveAPIKeyResponse
		json.NewDecoder(rw.Body).Decode(&resp)
		return resp
	}

	r1 := call()
	r2 := call()

	if r1.APIKey != r2.APIKey {
		t.Errorf("api_key differs across calls: %q vs %q", r1.APIKey, r2.APIKey)
	}
	if r1.HMACSecret != r2.HMACSecret {
		t.Errorf("hmac_secret differs across calls: %q vs %q", r1.HMACSecret, r2.HMACSecret)
	}
}

func TestDeriveAPIKey_NoAuth(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	h := testHandler(t, repo, &fakeVerifier{})

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"signature":"0xdeadbeef","timestamp":"1700000000"}`
	r := httptest.NewRequest(http.MethodPost, "/auth/derive-api-key", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// ---------------------------------------------------------------------------
// DELETE /auth/api-key
// ---------------------------------------------------------------------------

func TestRevokeAPIKey_Success(t *testing.T) {
	t.Parallel()

	addr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	rawKey := "test-api-key-value"
	keyHash := HashAPIKey(rawKey)

	repo := newFakeRepo()
	repo.apiKeys[keyHash] = &data.APIKey{
		KeyHash:     keyHash,
		UserAddress: addr,
	}

	h := testHandler(t, repo, &fakeVerifier{})

	body := `{"api_key":"` + rawKey + `"}`
	r := httptest.NewRequest(http.MethodDelete, "/auth/api-key", strings.NewReader(body))
	r = r.WithContext(WithUserAddress(r.Context(), addr))
	w := httptest.NewRecorder()

	h.revokeAPIKey(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusNoContent, w.Body.String())
	}
	if !repo.apiKeys[keyHash].Revoked {
		t.Error("expected api key to be marked revoked in repo")
	}
}

func TestRevokeAPIKey_NotFound(t *testing.T) {
	t.Parallel()

	addr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	repo := newFakeRepo()
	h := testHandler(t, repo, &fakeVerifier{})

	body := `{"api_key":"does-not-exist"}`
	r := httptest.NewRequest(http.MethodDelete, "/auth/api-key", strings.NewReader(body))
	r = r.WithContext(WithUserAddress(r.Context(), addr))
	w := httptest.NewRecorder()

	h.revokeAPIKey(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestRevokeAPIKey_MissingField(t *testing.T) {
	t.Parallel()

	addr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	repo := newFakeRepo()
	h := testHandler(t, repo, &fakeVerifier{})

	r := httptest.NewRequest(http.MethodDelete, "/auth/api-key", strings.NewReader("{}"))
	r = r.WithContext(WithUserAddress(r.Context(), addr))
	w := httptest.NewRecorder()

	h.revokeAPIKey(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ---------------------------------------------------------------------------
// GET /auth/api-keys
// ---------------------------------------------------------------------------

func TestListAPIKeys_WithKeys(t *testing.T) {
	t.Parallel()

	addr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	repo := newFakeRepo()
	repo.apiKeys["hash-one"] = &data.APIKey{KeyHash: "hash-one", UserAddress: addr}
	repo.apiKeys["hash-two"] = &data.APIKey{KeyHash: "hash-two", UserAddress: addr}

	h := testHandler(t, repo, &fakeVerifier{})

	r := httptest.NewRequest(http.MethodGet, "/auth/api-keys", nil)
	r = r.WithContext(WithUserAddress(r.Context(), addr))
	w := httptest.NewRecorder()

	h.listAPIKeys(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var items []apiKeyListItem
	if err := json.NewDecoder(w.Body).Decode(&items); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(items) != 2 {
		t.Errorf("items count = %d, want 2", len(items))
	}
}

func TestListAPIKeys_Empty(t *testing.T) {
	t.Parallel()

	addr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	repo := newFakeRepo()
	h := testHandler(t, repo, &fakeVerifier{})

	r := httptest.NewRequest(http.MethodGet, "/auth/api-keys", nil)
	r = r.WithContext(WithUserAddress(r.Context(), addr))
	w := httptest.NewRecorder()

	h.listAPIKeys(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	var items []apiKeyListItem
	body := strings.TrimSpace(w.Body.String())
	if err := json.Unmarshal([]byte(body), &items); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("items count = %d, want 0", len(items))
	}
}

func TestListAPIKeys_SecretsStripped(t *testing.T) {
	t.Parallel()

	addr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	repo := newFakeRepo()
	repo.apiKeys["hash-one"] = &data.APIKey{
		KeyHash:             "hash-one",
		UserAddress:         addr,
		HMACSecretEncrypted: "super-secret-ciphertext",
	}

	h := testHandler(t, repo, &fakeVerifier{})

	r := httptest.NewRequest(http.MethodGet, "/auth/api-keys", nil)
	r = r.WithContext(WithUserAddress(r.Context(), addr))
	w := httptest.NewRecorder()

	h.listAPIKeys(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	// The encrypted secret must never appear in the response body.
	if strings.Contains(w.Body.String(), "super-secret-ciphertext") {
		t.Error("response must not include HMACSecretEncrypted")
	}
}

func TestListAPIKeys_NoAuth(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	h := testHandler(t, repo, &fakeVerifier{})

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	r := httptest.NewRequest(http.MethodGet, "/auth/api-keys", nil)
	r.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}
