package auth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap/zaptest"

	"github.com/Shisa-Fosho/services/internal/platform/data"
)

// fakeAPIKeyReader is an in-memory APIKeyReader for tests.
type fakeAPIKeyReader struct {
	keys map[string]*APIKey
}

func (f *fakeAPIKeyReader) GetAPIKeyByHash(_ context.Context, keyHash string) (*APIKey, error) {
	k, ok := f.keys[keyHash]
	if !ok || k.Revoked || k.ExpiresAt.Before(time.Now()) {
		return nil, data.ErrNotFound
	}
	return k, nil
}

// signRequest signs an HTTP request with HMAC-SHA256 using the clob-client v5.8.2
// contract: URL-safe base64 output over `timestamp + method + path + body`, with
// the wire-format secret (URL-safe base64) decoded to raw bytes as the HMAC key.
func signRequest(r *http.Request, apiKey, hmacSecretB64, passphrase, body string) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	msg := BuildHMACMessage(ts, r.Method, r.URL.Path, body)
	secretBytes, err := base64.URLEncoding.DecodeString(hmacSecretB64)
	if err != nil {
		panic("signRequest: secret must be URL-safe base64: " + err.Error())
	}
	mac := hmac.New(sha256.New, secretBytes)
	mac.Write([]byte(msg))
	sig := base64.URLEncoding.EncodeToString(mac.Sum(nil))

	r.Header.Set(HeaderAPIKey, apiKey)
	r.Header.Set(HeaderTimestamp, ts)
	r.Header.Set(HeaderSignature, sig)
	r.Header.Set(HeaderPassphrase, passphrase)
}

// testAPIKeySetup stores a valid APIKey in the reader and returns
// (rawKey, hmacSecretB64, passphrase, encryptionKey).
func testAPIKeySetup(t *testing.T, reader *fakeAPIKeyReader, address string) (string, string, string, []byte) {
	t.Helper()
	encKey := []byte("test-encryption-key-32-bytes!!!!") // exactly 32 bytes

	rawKey := "test-api-key-hex-0123456789abcd"
	hmacSecretB64 := base64.URLEncoding.EncodeToString([]byte("test-hmac-secret-exactly-32bytes"))
	passphrase := "test-passphrase-0123456789abcdef"
	keyHash := HashAPIKey(rawKey)

	encrypted, err := EncryptSecret(encKey, hmacSecretB64)
	if err != nil {
		t.Fatalf("encrypting secret: %v", err)
	}

	reader.keys[keyHash] = &APIKey{
		KeyHash:             keyHash,
		UserAddress:         address,
		HMACSecretEncrypted: encrypted,
		PassphraseHash:      HashAPIKey(passphrase),
		ExpiresAt:           time.Now().Add(24 * time.Hour),
	}

	return rawKey, hmacSecretB64, passphrase, encKey
}

// newAPIKeyTestHandler wraps an echo handler with AuthenticateAPIKey.
func newAPIKeyTestHandler(t *testing.T, reader APIKeyReader, encKey []byte) http.Handler {
	t.Helper()
	logger := zaptest.NewLogger(t)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(UserAddressFrom(r.Context())))
	})
	return AuthenticateAPIKey(reader, encKey, logger)(inner)
}

func TestAuthenticateAPIKey_ValidRequest(t *testing.T) {
	t.Parallel()
	addr := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	reader := &fakeAPIKeyReader{keys: make(map[string]*APIKey)}
	rawKey, hmacSecret, passphrase, encKey := testAPIKeySetup(t, reader, addr)
	handler := newAPIKeyTestHandler(t, reader, encKey)

	body := `{"order":"buy"}`
	r := httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(body))
	signRequest(r, rawKey, hmacSecret, passphrase, body)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if got := w.Body.String(); got != addr {
		t.Errorf("address = %q, want %q", got, addr)
	}
}

// TestAuthenticateAPIKey_RejectsJWT enforces the architectural rule that a
// CLOB-protected endpoint rejects JWT Bearer tokens even if they're otherwise
// valid. This is the contract-change check that prevents accidental
// reintroduction of a combined "accept either" middleware.
//
// The specific token shape doesn't matter — AuthenticateAPIKey rejects before
// ever looking at Authorization. A plausible Bearer value keeps the intent
// readable without pulling the JWT package across the service boundary.
func TestAuthenticateAPIKey_RejectsJWT(t *testing.T) {
	t.Parallel()
	encKey := []byte("test-encryption-key-32-bytes!!!!")
	reader := &fakeAPIKeyReader{keys: make(map[string]*APIKey)}
	handler := newAPIKeyTestHandler(t, reader, encKey)

	r := httptest.NewRequest(http.MethodGet, "/orders", nil)
	r.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiJ9.valid.looking.jwt")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d; CLOB middleware must not accept JWT Bearer", w.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(w.Body.String(), "POLY_API_KEY") {
		t.Errorf("body = %q, want message that mentions POLY_API_KEY so clients know which auth this endpoint expects", w.Body.String())
	}
}

func TestAuthenticateAPIKey_MissingAPIKeyHeader(t *testing.T) {
	t.Parallel()
	encKey := []byte("test-encryption-key-32-bytes!!!!")
	reader := &fakeAPIKeyReader{keys: make(map[string]*APIKey)}
	handler := newAPIKeyTestHandler(t, reader, encKey)

	r := httptest.NewRequest(http.MethodGet, "/orders", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(w.Body.String(), "POLY_API_KEY") {
		t.Errorf("body = %q, want mention of POLY_API_KEY", w.Body.String())
	}
}

func TestAuthenticateAPIKey_TimestampDrift(t *testing.T) {
	t.Parallel()
	addr := "0xdddddddddddddddddddddddddddddddddddddd"
	reader := &fakeAPIKeyReader{keys: make(map[string]*APIKey)}
	rawKey, hmacSecretB64, _, encKey := testAPIKeySetup(t, reader, addr)
	handler := newAPIKeyTestHandler(t, reader, encKey)

	staleTS := strconv.FormatInt(time.Now().Add(-10*time.Second).Unix(), 10)
	body := ""
	msg := BuildHMACMessage(staleTS, http.MethodGet, "/orders", body)
	secretBytes, _ := base64.URLEncoding.DecodeString(hmacSecretB64)
	mac := hmac.New(sha256.New, secretBytes)
	mac.Write([]byte(msg))
	sig := base64.URLEncoding.EncodeToString(mac.Sum(nil))

	r := httptest.NewRequest(http.MethodGet, "/orders", nil)
	r.Header.Set(HeaderAPIKey, rawKey)
	r.Header.Set(HeaderTimestamp, staleTS)
	r.Header.Set(HeaderSignature, sig)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(w.Body.String(), "timestamp drift") {
		t.Errorf("body = %q, want 'timestamp drift'", w.Body.String())
	}
}

func TestAuthenticateAPIKey_InvalidHMAC(t *testing.T) {
	t.Parallel()
	addr := "0xffffffffffffffffffffffffffffffffffffffff"
	reader := &fakeAPIKeyReader{keys: make(map[string]*APIKey)}
	rawKey, _, passphrase, encKey := testAPIKeySetup(t, reader, addr)
	handler := newAPIKeyTestHandler(t, reader, encKey)

	r := httptest.NewRequest(http.MethodGet, "/orders", nil)
	r.Header.Set(HeaderAPIKey, rawKey)
	r.Header.Set(HeaderTimestamp, strconv.FormatInt(time.Now().Unix(), 10))
	r.Header.Set(HeaderSignature, "bad-signature")
	r.Header.Set(HeaderPassphrase, passphrase)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(w.Body.String(), "invalid signature") {
		t.Errorf("body = %q, want 'invalid signature'", w.Body.String())
	}
}

func TestAuthenticateAPIKey_RevokedKey(t *testing.T) {
	t.Parallel()
	addr := "0x1111111111111111111111111111111111111111"
	reader := &fakeAPIKeyReader{keys: make(map[string]*APIKey)}
	rawKey, hmacSecret, passphrase, encKey := testAPIKeySetup(t, reader, addr)

	keyHash := HashAPIKey(rawKey)
	reader.keys[keyHash].Revoked = true

	handler := newAPIKeyTestHandler(t, reader, encKey)

	body := ""
	r := httptest.NewRequest(http.MethodGet, "/orders", nil)
	signRequest(r, rawKey, hmacSecret, passphrase, body)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(w.Body.String(), "invalid api key") {
		t.Errorf("body = %q, want 'invalid api key'", w.Body.String())
	}
}

func TestAuthenticateAPIKey_MissingPassphrase(t *testing.T) {
	t.Parallel()
	addr := "0x2222222222222222222222222222222222222222"
	reader := &fakeAPIKeyReader{keys: make(map[string]*APIKey)}
	rawKey, hmacSecretB64, _, encKey := testAPIKeySetup(t, reader, addr)
	handler := newAPIKeyTestHandler(t, reader, encKey)

	body := ""
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	msg := BuildHMACMessage(ts, http.MethodGet, "/orders", body)
	secretBytes, _ := base64.URLEncoding.DecodeString(hmacSecretB64)
	mac := hmac.New(sha256.New, secretBytes)
	mac.Write([]byte(msg))
	sig := base64.URLEncoding.EncodeToString(mac.Sum(nil))

	r := httptest.NewRequest(http.MethodGet, "/orders", nil)
	r.Header.Set(HeaderAPIKey, rawKey)
	r.Header.Set(HeaderTimestamp, ts)
	r.Header.Set(HeaderSignature, sig)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(w.Body.String(), "invalid passphrase") {
		t.Errorf("body = %q, want 'invalid passphrase'", w.Body.String())
	}
}

func TestAuthenticateAPIKey_WrongPassphrase(t *testing.T) {
	t.Parallel()
	addr := "0x3333333333333333333333333333333333333333"
	reader := &fakeAPIKeyReader{keys: make(map[string]*APIKey)}
	rawKey, hmacSecret, _, encKey := testAPIKeySetup(t, reader, addr)
	handler := newAPIKeyTestHandler(t, reader, encKey)

	body := ""
	r := httptest.NewRequest(http.MethodGet, "/orders", nil)
	signRequest(r, rawKey, hmacSecret, "definitely-not-the-right-passphrase", body)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	if !strings.Contains(w.Body.String(), "invalid passphrase") {
		t.Errorf("body = %q, want 'invalid passphrase'", w.Body.String())
	}
}
