package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	"github.com/Shisa-Fosho/services/internal/data"
)

// --- Existing Authenticate (JWT-only) tests ---

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

// --- Combined AuthMiddleware tests ---

// signRequest is a test helper that signs an HTTP request with HMAC-SHA256.
func signRequest(r *http.Request, apiKey, hmacSecret, body string) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	msg := BuildHMACMessage(ts, r.Method, r.URL.Path, body)
	mac := hmac.New(sha256.New, []byte(hmacSecret))
	mac.Write([]byte(msg))
	sig := hex.EncodeToString(mac.Sum(nil))

	r.Header.Set(HeaderAPIKey, apiKey)
	r.Header.Set(HeaderTimestamp, ts)
	r.Header.Set(HeaderSignature, sig)
	r.Header.Set(HeaderNonce, "1")
}

// testAPIKeySetup creates a stored API key and returns (rawKey, hmacSecret, encryptionKey).
func testAPIKeySetup(t *testing.T, repo *fakeRepo, address string) (string, string, []byte) {
	t.Helper()
	encKey := []byte("test-encryption-key-32-bytes!!!!") // exactly 32 bytes

	rawKey := "test-api-key-hex-0123456789abcd"
	hmacSecret := "test-hmac-secret"
	keyHash := HashAPIKey(rawKey)

	encrypted, err := EncryptSecret(encKey, hmacSecret)
	if err != nil {
		t.Fatalf("encrypting secret: %v", err)
	}

	repo.apiKeys[keyHash] = &data.APIKey{
		KeyHash:             keyHash,
		UserAddress:         address,
		HMACSecretEncrypted: encrypted,
		ExpiresAt:           time.Now().Add(24 * time.Hour),
	}

	return rawKey, hmacSecret, encKey
}

// newAuthMiddlewareHandler creates a test handler wrapped with AuthMiddleware.
func newAuthMiddlewareHandler(t *testing.T, repo *fakeRepo, encKey []byte) (http.Handler, *JWTManager, *NonceTracker) {
	t.Helper()
	jwtMgr, err := NewJWTManager(testJWTConfig())
	if err != nil {
		t.Fatalf("creating JWT manager: %v", err)
	}
	logger := zaptest.NewLogger(t)
	nonces := NewNonceTracker()
	t.Cleanup(nonces.Stop)

	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(UserAddressFrom(r.Context())))
	})
	handler := Middleware(jwtMgr, repo, encKey, nonces, logger)(inner)
	return handler, jwtMgr, nonces
}

func TestAuthMiddleware_ValidAPIKey(t *testing.T) {
	t.Parallel()
	addr := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	repo := &fakeRepo{apiKeys: make(map[string]*data.APIKey)}
	rawKey, hmacSecret, encKey := testAPIKeySetup(t, repo, addr)
	handler, _, _ := newAuthMiddlewareHandler(t, repo, encKey)

	body := `{"order":"buy"}`
	r := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(body))
	signRequest(r, rawKey, hmacSecret, body)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Body.String(); got != addr {
		t.Errorf("address = %q, want %q", got, addr)
	}
}

func TestAuthMiddleware_ValidJWT(t *testing.T) {
	t.Parallel()
	addr := "0xcccccccccccccccccccccccccccccccccccccccc"
	encKey := []byte("test-encryption-key-32-bytes!!!!")
	repo := &fakeRepo{apiKeys: make(map[string]*data.APIKey)}
	handler, jwtMgr, _ := newAuthMiddlewareHandler(t, repo, encKey)

	token, _ := jwtMgr.IssueAccessToken(addr)
	r := httptest.NewRequest(http.MethodGet, "/orders", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Body.String(); got != addr {
		t.Errorf("address = %q, want %q", got, addr)
	}
}

func TestAuthMiddleware_APIKeyTakesPrecedence(t *testing.T) {
	t.Parallel()
	apiKeyAddr := "0xcccccccccccccccccccccccccccccccccccccccc"
	jwtAddr := "0xdddddddddddddddddddddddddddddddddddddd"

	repo := &fakeRepo{apiKeys: make(map[string]*data.APIKey)}
	rawKey, hmacSecret, encKey := testAPIKeySetup(t, repo, apiKeyAddr)
	handler, jwtMgr, _ := newAuthMiddlewareHandler(t, repo, encKey)
	token, _ := jwtMgr.IssueAccessToken(jwtAddr)
	body := `{"order":"buy"}`
	r := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	signRequest(r, rawKey, hmacSecret, body)

	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if got := w.Body.String(); got != apiKeyAddr {
		t.Errorf("address = %q, want %q", got, apiKeyAddr)
	}

}

func TestAuthMiddleware_MissingCredentials(t *testing.T) {
	t.Parallel()
	encKey := []byte("test-encryption-key-32-bytes!!!!")
	repo := &fakeRepo{apiKeys: make(map[string]*data.APIKey)}
	handler, _, _ := newAuthMiddlewareHandler(t, repo, encKey)

	r := httptest.NewRequest(http.MethodGet, "/orders", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(w.Body.String(), "missing credentials") {
		t.Errorf("body = %q, want 'missing credentials'", w.Body.String())
	}
}

func TestAuthMiddleware_TimestampDrift(t *testing.T) {
	t.Parallel()
	addr := "0xdddddddddddddddddddddddddddddddddddddd"
	repo := &fakeRepo{apiKeys: make(map[string]*data.APIKey)}
	rawKey, hmacSecret, encKey := testAPIKeySetup(t, repo, addr)
	handler, _, _ := newAuthMiddlewareHandler(t, repo, encKey)

	// Use a timestamp 10 seconds in the past.
	staleTS := strconv.FormatInt(time.Now().Add(-10*time.Second).Unix(), 10)
	body := ""
	msg := BuildHMACMessage(staleTS, http.MethodGet, "/orders", body)
	mac := hmac.New(sha256.New, []byte(hmacSecret))
	mac.Write([]byte(msg))
	sig := hex.EncodeToString(mac.Sum(nil))

	r := httptest.NewRequest(http.MethodGet, "/orders", nil)
	r.Header.Set(HeaderAPIKey, rawKey)
	r.Header.Set(HeaderTimestamp, staleTS)
	r.Header.Set(HeaderSignature, sig)
	r.Header.Set(HeaderNonce, "1")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(w.Body.String(), "timestamp drift") {
		t.Errorf("body = %q, want 'timestamp drift'", w.Body.String())
	}
}

func TestAuthMiddleware_NonceReplay(t *testing.T) {
	t.Parallel()
	addr := "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	repo := &fakeRepo{apiKeys: make(map[string]*data.APIKey)}
	rawKey, hmacSecret, encKey := testAPIKeySetup(t, repo, addr)
	handler, _, _ := newAuthMiddlewareHandler(t, repo, encKey)

	body := ""
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	msg := BuildHMACMessage(ts, http.MethodGet, "/orders", body)
	mac := hmac.New(sha256.New, []byte(hmacSecret))
	mac.Write([]byte(msg))
	sig := hex.EncodeToString(mac.Sum(nil))

	makeReq := func() *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/orders", nil)
		r.Header.Set(HeaderAPIKey, rawKey)
		r.Header.Set(HeaderTimestamp, ts)
		r.Header.Set(HeaderSignature, sig)
		r.Header.Set(HeaderNonce, "same-nonce")
		return r
	}

	// First request should succeed.
	w1 := httptest.NewRecorder()
	handler.ServeHTTP(w1, makeReq())
	if w1.Code != http.StatusOK {
		t.Fatalf("first request: status = %d, want %d", w1.Code, http.StatusOK)
	}

	// Second identical request should be rejected.
	w2 := httptest.NewRecorder()
	handler.ServeHTTP(w2, makeReq())
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("replay: status = %d, want %d", w2.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(w2.Body.String(), "replayed") {
		t.Errorf("body = %q, want 'replayed'", w2.Body.String())
	}
}

func TestAuthMiddleware_InvalidHMAC(t *testing.T) {
	t.Parallel()
	addr := "0xffffffffffffffffffffffffffffffffffffffff"
	repo := &fakeRepo{apiKeys: make(map[string]*data.APIKey)}
	rawKey, _, encKey := testAPIKeySetup(t, repo, addr)
	handler, _, _ := newAuthMiddlewareHandler(t, repo, encKey)

	r := httptest.NewRequest(http.MethodGet, "/orders", nil)
	r.Header.Set(HeaderAPIKey, rawKey)
	r.Header.Set(HeaderTimestamp, strconv.FormatInt(time.Now().Unix(), 10))
	r.Header.Set(HeaderSignature, "bad-signature")
	r.Header.Set(HeaderNonce, "1")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(w.Body.String(), "invalid signature") {
		t.Errorf("body = %q, want 'invalid signature'", w.Body.String())
	}
}

func TestAuthMiddleware_RevokedKey(t *testing.T) {
	t.Parallel()
	addr := "0x1111111111111111111111111111111111111111"
	repo := &fakeRepo{apiKeys: make(map[string]*data.APIKey)}
	rawKey, hmacSecret, encKey := testAPIKeySetup(t, repo, addr)

	// Revoke the key.
	keyHash := HashAPIKey(rawKey)
	repo.apiKeys[keyHash].Revoked = true

	handler, _, _ := newAuthMiddlewareHandler(t, repo, encKey)

	body := ""
	r := httptest.NewRequest(http.MethodGet, "/orders", nil)
	signRequest(r, rawKey, hmacSecret, body)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(w.Body.String(), "invalid api key") {
		t.Errorf("body = %q, want 'invalid api key'", w.Body.String())
	}
}
