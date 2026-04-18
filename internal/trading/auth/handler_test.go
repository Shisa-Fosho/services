package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"go.uber.org/zap/zaptest"

	"github.com/Shisa-Fosho/services/internal/platform/data"
)

// fakeRepo is an in-memory APIKeyRepository implementation for tests.
type fakeRepo struct {
	apiKeys map[string]*APIKey
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{apiKeys: make(map[string]*APIKey)}
}

func (r *fakeRepo) UpsertAPIKey(_ context.Context, key *APIKey) error {
	r.apiKeys[key.KeyHash] = key
	return nil
}

func (r *fakeRepo) GetAPIKeyByHash(_ context.Context, keyHash string) (*APIKey, error) {
	k, ok := r.apiKeys[keyHash]
	if !ok || k.Revoked || k.ExpiresAt.Before(time.Now()) {
		return nil, data.ErrNotFound
	}
	return k, nil
}

func (r *fakeRepo) GetAPIKeysByUser(_ context.Context, userAddress string) ([]*APIKey, error) {
	var result []*APIKey
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

// testConfig returns a valid APIKeyConfig for tests.
func testConfig() APIKeyConfig {
	return APIKeyConfig{
		DerivationSecret: []byte("test-derivation-secret-32-bytes!"),
		EncryptionKey:    []byte("test-encryption-key-32-bytes!!xx"),
		ChainID:          137,
	}
}

// testHandler constructs a Handler wired to the given fakeRepo.
func testHandler(t *testing.T, repo *fakeRepo) *Handler {
	t.Helper()
	return NewHandler(zaptest.NewLogger(t), repo, testConfig())
}

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

// l1Request builds a GET /auth/derive-api-key request with the L1 headers
// the SDK's createL1Headers function would populate.
func l1Request(t *testing.T, address, sigHex, timestamp, nonce string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/auth/derive-api-key", nil)
	r.Header.Set(HeaderAddress, address)
	r.Header.Set(HeaderSignature, sigHex)
	r.Header.Set(HeaderTimestamp, timestamp)
	r.Header.Set(HeaderNonce, nonce)
	return r
}

// keysOf lists map keys for readable assertion errors.
func keysOf(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ---------------------------------------------------------------------------
// GET /auth/derive-api-key
// ---------------------------------------------------------------------------

func TestDeriveAPIKey_HappyPath(t *testing.T) {
	t.Parallel()

	address, sigHex := signedEIP712(t)
	repo := newFakeRepo()
	h := testHandler(t, repo)

	r := l1Request(t, address, sigHex, "1700000000", "0")
	w := httptest.NewRecorder()

	h.deriveAPIKey(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		APIKey     string `json:"apiKey"`
		Secret     string `json:"secret"`
		Passphrase string `json:"passphrase"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.APIKey == "" || resp.Secret == "" || resp.Passphrase == "" {
		t.Errorf("response fields empty: %+v", resp)
	}
	if strings.Contains(w.Body.String(), "expires_at") || strings.Contains(w.Body.String(), "expiresAt") {
		t.Errorf("response must not include expires_at field, got: %s", w.Body.String())
	}
	if len(repo.apiKeys) != 1 {
		t.Errorf("repo apiKeys count = %d, want 1", len(repo.apiKeys))
	}
	stored := repo.apiKeys[HashAPIKey(resp.APIKey)]
	if stored == nil {
		t.Fatal("derived key not stored")
	}
	if stored.PassphraseHash != HashAPIKey(resp.Passphrase) {
		t.Error("stored passphrase hash does not match hash of returned passphrase")
	}
}

func TestDeriveAPIKey_SDKShapedRequest(t *testing.T) {
	t.Parallel()

	address, sigHex := signedEIP712(t)
	repo := newFakeRepo()
	h := testHandler(t, repo)

	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	r := httptest.NewRequest(http.MethodGet, "/auth/derive-api-key", nil)
	r.Header.Set("POLY_ADDRESS", address)
	r.Header.Set("POLY_SIGNATURE", sigHex)
	r.Header.Set("POLY_TIMESTAMP", "1700000000")
	r.Header.Set("POLY_NONCE", "0")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	for _, field := range []string{"apiKey", "secret", "passphrase"} {
		if _, ok := raw[field]; !ok {
			t.Errorf("response missing SDK field %q; got keys %v", field, keysOf(raw))
		}
	}
	for _, field := range []string{"api_key", "hmac_secret", "expires_at", "expiresAt"} {
		if _, ok := raw[field]; ok {
			t.Errorf("response contains unexpected field %q; got keys %v", field, keysOf(raw))
		}
	}
}

func TestDeriveAPIKey_DefaultNonce(t *testing.T) {
	t.Parallel()

	address, sigHex := signedEIP712(t)
	repo := newFakeRepo()
	h := testHandler(t, repo)

	r := httptest.NewRequest(http.MethodGet, "/auth/derive-api-key", nil)
	r.Header.Set(HeaderAddress, address)
	r.Header.Set(HeaderSignature, sigHex)
	r.Header.Set(HeaderTimestamp, "1700000000")
	w := httptest.NewRecorder()

	h.deriveAPIKey(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestDeriveAPIKey_MissingHeaders(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	h := testHandler(t, repo)

	r := httptest.NewRequest(http.MethodGet, "/auth/derive-api-key", nil)
	w := httptest.NewRecorder()

	h.deriveAPIKey(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestDeriveAPIKey_InvalidSignature(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	h := testHandler(t, repo)

	addr := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	badSig := "0x" + hex.EncodeToString(make([]byte, 65))

	r := l1Request(t, addr, badSig, "1700000000", "0")
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
	h := testHandler(t, repo)

	call := func() (apiKey, secret, passphrase string) {
		rq := l1Request(t, address, sigHex, "1700000000", "0")
		rw := httptest.NewRecorder()
		h.deriveAPIKey(rw, rq)
		if rw.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body: %s", rw.Code, http.StatusOK, rw.Body.String())
		}
		var resp struct {
			APIKey     string `json:"apiKey"`
			Secret     string `json:"secret"`
			Passphrase string `json:"passphrase"`
		}
		json.NewDecoder(rw.Body).Decode(&resp)
		return resp.APIKey, resp.Secret, resp.Passphrase
	}

	k1, s1, p1 := call()
	k2, s2, p2 := call()
	if k1 != k2 || s1 != s2 || p1 != p2 {
		t.Errorf("derive is not idempotent: (%q,%q,%q) vs (%q,%q,%q)", k1, s1, p1, k2, s2, p2)
	}
}

// ---------------------------------------------------------------------------
// DELETE /auth/api-key  (L2 HMAC auth)
// ---------------------------------------------------------------------------

// seedAPIKey inserts a valid API key into the fake repo and returns the raw
// credentials needed to HMAC-sign a request for it.
func seedAPIKey(t *testing.T, repo *fakeRepo, address string) (rawKey, secretB64, passphrase string) {
	t.Helper()
	rawKey = "test-api-key-0123456789abcdef"
	secretB64 = base64.URLEncoding.EncodeToString([]byte("test-hmac-secret-exactly-32bytes"))
	passphrase = "test-passphrase-0123456789abcdef"

	cfg := testConfig()
	encrypted, err := EncryptSecret(cfg.EncryptionKey, secretB64)
	if err != nil {
		t.Fatalf("encrypting secret: %v", err)
	}

	repo.apiKeys[HashAPIKey(rawKey)] = &APIKey{
		KeyHash:             HashAPIKey(rawKey),
		UserAddress:         address,
		HMACSecretEncrypted: encrypted,
		PassphraseHash:      HashAPIKey(passphrase),
		ExpiresAt:           time.Now().Add(24 * time.Hour),
	}
	return rawKey, secretB64, passphrase
}

// signL2Request sets L2 HMAC headers on r with a signature over its
// method+path+body. Mirrors clob-client v5.8.2 signing rules.
func signL2Request(t *testing.T, r *http.Request, apiKey, secretB64, passphrase, body string) {
	t.Helper()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	msg := BuildHMACMessage(ts, r.Method, r.URL.Path, body)
	secretBytes, _ := base64.URLEncoding.DecodeString(secretB64)
	mac := hmac.New(sha256.New, secretBytes)
	mac.Write([]byte(msg))
	sig := base64.URLEncoding.EncodeToString(mac.Sum(nil))

	r.Header.Set(HeaderAPIKey, apiKey)
	r.Header.Set(HeaderTimestamp, ts)
	r.Header.Set(HeaderSignature, sig)
	r.Header.Set(HeaderPassphrase, passphrase)
}

func TestRevokeAPIKey_Success(t *testing.T) {
	t.Parallel()

	addr := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Hex()
	rawKeyToRevoke := "target-key-to-revoke"
	keyHash := HashAPIKey(rawKeyToRevoke)

	repo := newFakeRepo()
	repo.apiKeys[keyHash] = &APIKey{
		KeyHash:     keyHash,
		UserAddress: addr,
		ExpiresAt:   time.Now().Add(24 * time.Hour),
	}

	callerKey, callerSecret, callerPass := seedAPIKey(t, repo, addr)

	h := testHandler(t, repo)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"api_key":"` + rawKeyToRevoke + `"}`
	r := httptest.NewRequest(http.MethodDelete, "/auth/api-key", strings.NewReader(body))
	signL2Request(t, r, callerKey, callerSecret, callerPass, body)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusNoContent {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusNoContent, w.Body.String())
	}
	if !repo.apiKeys[keyHash].Revoked {
		t.Error("expected api key to be marked revoked")
	}
}

func TestRevokeAPIKey_NotFound(t *testing.T) {
	t.Parallel()

	addr := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Hex()
	repo := newFakeRepo()
	callerKey, callerSecret, callerPass := seedAPIKey(t, repo, addr)

	h := testHandler(t, repo)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	body := `{"api_key":"does-not-exist"}`
	r := httptest.NewRequest(http.MethodDelete, "/auth/api-key", strings.NewReader(body))
	signL2Request(t, r, callerKey, callerSecret, callerPass, body)
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestRevokeAPIKey_MissingField(t *testing.T) {
	t.Parallel()

	addr := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Hex()
	repo := newFakeRepo()
	callerKey, callerSecret, callerPass := seedAPIKey(t, repo, addr)

	h := testHandler(t, repo)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	r := httptest.NewRequest(http.MethodDelete, "/auth/api-key", strings.NewReader("{}"))
	signL2Request(t, r, callerKey, callerSecret, callerPass, "{}")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestRevokeAPIKey_RejectsJWT(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	h := testHandler(t, repo)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	r := httptest.NewRequest(http.MethodDelete, "/auth/api-key", strings.NewReader(`{"api_key":"x"}`))
	r.Header.Set("Authorization", "Bearer some.jwt.token")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d; CLOB routes must not accept JWT", w.Code, http.StatusUnauthorized)
	}
}

// ---------------------------------------------------------------------------
// GET /auth/api-keys  (L2 HMAC auth)
// ---------------------------------------------------------------------------

func TestListAPIKeys_WithKeys(t *testing.T) {
	t.Parallel()

	addr := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Hex()
	repo := newFakeRepo()

	repo.apiKeys["hash-one"] = &APIKey{
		KeyHash: "hash-one", UserAddress: addr, ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	repo.apiKeys["hash-two"] = &APIKey{
		KeyHash: "hash-two", UserAddress: addr, ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	callerKey, callerSecret, callerPass := seedAPIKey(t, repo, addr)

	h := testHandler(t, repo)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	r := httptest.NewRequest(http.MethodGet, "/auth/api-keys", nil)
	signL2Request(t, r, callerKey, callerSecret, callerPass, "")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	var items []apiKeyListItem
	if err := json.NewDecoder(w.Body).Decode(&items); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(items) != 3 {
		t.Errorf("items count = %d, want 3", len(items))
	}
}

func TestListAPIKeys_SecretsStripped(t *testing.T) {
	t.Parallel()

	addr := common.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa").Hex()
	repo := newFakeRepo()
	repo.apiKeys["sensitive"] = &APIKey{
		KeyHash:             "sensitive",
		UserAddress:         addr,
		HMACSecretEncrypted: "super-secret-ciphertext",
		ExpiresAt:           time.Now().Add(24 * time.Hour),
	}
	callerKey, callerSecret, callerPass := seedAPIKey(t, repo, addr)

	h := testHandler(t, repo)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	r := httptest.NewRequest(http.MethodGet, "/auth/api-keys", nil)
	signL2Request(t, r, callerKey, callerSecret, callerPass, "")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if strings.Contains(w.Body.String(), "super-secret-ciphertext") {
		t.Error("response must not include HMACSecretEncrypted")
	}
}

func TestListAPIKeys_RejectsJWT(t *testing.T) {
	t.Parallel()

	repo := newFakeRepo()
	h := testHandler(t, repo)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	r := httptest.NewRequest(http.MethodGet, "/auth/api-keys", nil)
	r.Header.Set("Authorization", "Bearer some.jwt.token")
	w := httptest.NewRecorder()

	mux.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d; CLOB routes must not accept JWT", w.Code, http.StatusUnauthorized)
	}
}
