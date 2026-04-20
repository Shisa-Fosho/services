package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"

	"github.com/Shisa-Fosho/services/internal/shared/httputil"
)

// Polymarket-compatible request headers.
//
// L1 (EIP-712 wallet auth, used for /auth/derive-api-key):
//
//	POLY_ADDRESS, POLY_SIGNATURE, POLY_TIMESTAMP, POLY_NONCE
//
// L2 (HMAC API-key auth, used for trading endpoints):
//
//	POLY_ADDRESS, POLY_SIGNATURE, POLY_TIMESTAMP, POLY_API_KEY, POLY_PASSPHRASE
const (
	HeaderAddress    = "POLY_ADDRESS"
	HeaderAPIKey     = "POLY_API_KEY" //nolint:gosec // header name, not a credential
	HeaderSignature  = "POLY_SIGNATURE"
	HeaderTimestamp  = "POLY_TIMESTAMP"
	HeaderNonce      = "POLY_NONCE"
	HeaderPassphrase = "POLY_PASSPHRASE"
)

// maxTimestampDrift is the maximum allowed age of a signed request.
// Replay protection within this window is accepted as a Polymarket-compatible trade-off.
const maxTimestampDrift = 5 * time.Second

// MiddlewareOption configures an AuthenticateAPIKey middleware chain.
type MiddlewareOption func(*middlewareOptions)

type middlewareOptions struct {
	onAuthFailure func(*http.Request)
}

// WithAuthFailureHook registers a callback invoked on credential-verify
// failures: unknown API key, passphrase mismatch, timestamp drift, HMAC
// signature mismatch, secret decrypt/decode failures. Does NOT fire on shape
// errors (missing POLY_API_KEY, missing signature headers, invalid-timestamp
// parse, body read error) — those are probes, not brute-force attempts.
func WithAuthFailureHook(hook func(*http.Request)) MiddlewareOption {
	return func(opts *middlewareOptions) { opts.onAuthFailure = hook }
}

// AuthenticateAPIKey returns HTTP middleware that validates an L2 HMAC-signed
// request (POLY_API_KEY, POLY_SIGNATURE, POLY_TIMESTAMP, POLY_PASSPHRASE)
// against a stored API key looked up via the reader. Used by Polymarket-compat
// CLOB endpoints (trade placement, listing keys, revoking keys). On success it
// injects the authenticated user address into the request context.
//
// This middleware is HMAC-only. It does NOT fall back to JWT. A JWT bearer
// token on a CLOB-protected route is rejected with 401 — enforcement of the
// architectural rule that CLOB endpoints speak HMAC exclusively.
func AuthenticateAPIKey(reader APIKeyReader, encryptionKey []byte, logger *zap.Logger, opts ...MiddlewareOption) func(http.Handler) http.Handler {
	options := &middlewareOptions{}
	for _, option := range opts {
		option(options)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get(HeaderAPIKey)
			if apiKey == "" {
				httputil.ErrorResponse(w, http.StatusUnauthorized, "missing POLY_API_KEY header (this endpoint requires L2 HMAC auth)")
				return
			}
			address, ok := authenticateAPIKey(w, r, apiKey, reader, encryptionKey, logger, options.onAuthFailure)
			if !ok {
				return
			}
			ctx := WithUserAddress(r.Context(), address)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// authenticateAPIKey validates an HMAC-signed API key request. Returns the
// user address on success, or writes a 401 response and returns false.
// onAuthFailure (if non-nil) fires only on credential-verify failures — see
// WithAuthFailureHook for the precise classification.
func authenticateAPIKey(w http.ResponseWriter, r *http.Request, apiKey string, reader APIKeyReader, encryptionKey []byte, logger *zap.Logger, onAuthFailure func(*http.Request)) (string, bool) {
	fail := func() {
		if onAuthFailure != nil {
			onAuthFailure(r)
		}
	}

	timestamp := r.Header.Get(HeaderTimestamp)
	signature := r.Header.Get(HeaderSignature)

	if timestamp == "" || signature == "" {
		httputil.ErrorResponse(w, http.StatusUnauthorized, "missing signature headers")
		return "", false
	}

	// Validate timestamp drift.
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		httputil.ErrorResponse(w, http.StatusUnauthorized, "invalid timestamp")
		return "", false
	}
	if drift := time.Since(time.Unix(ts, 0)).Abs(); drift > maxTimestampDrift {
		fail()
		httputil.ErrorResponse(w, http.StatusUnauthorized, "timestamp drift too large")
		return "", false
	}

	// Look up API key.
	keyHash := HashAPIKey(apiKey)
	stored, err := reader.GetAPIKeyByHash(r.Context(), keyHash)
	if err != nil {
		logger.Debug("api key lookup failed", zap.Error(err))
		fail()
		httputil.ErrorResponse(w, http.StatusUnauthorized, "invalid api key")
		return "", false
	}

	// Verify passphrase (second factor alongside HMAC secret).
	passphrase := r.Header.Get(HeaderPassphrase)
	if !hmac.Equal([]byte(HashAPIKey(passphrase)), []byte(stored.PassphraseHash)) {
		fail()
		httputil.ErrorResponse(w, http.StatusUnauthorized, "invalid passphrase")
		return "", false
	}

	// Decrypt HMAC secret and verify signature. The stored secret is URL-safe
	// base64 (matching the clob-client contract); decode it to raw bytes to
	// use as the HMAC-SHA256 key.
	secret, err := DecryptSecret(encryptionKey, stored.HMACSecretEncrypted)
	if err != nil {
		logger.Error("decrypting hmac secret", zap.Error(err))
		fail()
		httputil.ErrorResponse(w, http.StatusUnauthorized, "invalid api key")
		return "", false
	}
	secretBytes, err := base64.URLEncoding.DecodeString(secret)
	if err != nil {
		logger.Error("decoding hmac secret", zap.Error(err))
		fail()
		httputil.ErrorResponse(w, http.StatusUnauthorized, "invalid api key")
		return "", false
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, "failed to read request body")
		return "", false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	if !VerifyHMACSignature(secretBytes, timestamp, r.Method, r.URL.Path, string(body), signature) {
		fail()
		httputil.ErrorResponse(w, http.StatusUnauthorized, "invalid signature")
		return "", false
	}

	return stored.UserAddress, true
}

// BuildHMACMessage constructs the signing payload: timestamp + method + path + body.
func BuildHMACMessage(timestamp, method, path, body string) string {
	return timestamp + method + path + body
}

// VerifyHMACSignature verifies an HMAC-SHA256 signature over the request payload.
//
// Contract (must match Polymarket clob-client v5.8.2 buildPolyHmacSignature):
//   - key is the raw-bytes HMAC key (caller already base64url-decoded it).
//   - The signed message is BuildHMACMessage(timestamp, method, path, body).
//   - The expected signature is URL-safe base64 of the HMAC output, with '='
//     padding preserved (base64.URLEncoding, NOT RawURLEncoding).
//   - Comparison must be constant-time (hmac.Equal).
func VerifyHMACSignature(key []byte, timestamp, method, path, body, signature string) bool {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(BuildHMACMessage(timestamp, method, path, body)))

	sig := base64.URLEncoding.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(sig), []byte(signature))
}
