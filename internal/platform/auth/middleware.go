package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/Shisa-Fosho/services/internal/data"
	"github.com/Shisa-Fosho/services/internal/platform/httputil"
)

// Polymarket-compatible request headers for API key authentication.
const (
	HeaderAPIKey    = "POLY_API_KEY" //nolint:gosec // header name, not a credential
	HeaderSignature = "POLY_SIGNATURE"
	HeaderTimestamp = "POLY_TIMESTAMP"
	HeaderNonce     = "POLY_NONCE"
)

// maxTimestampDrift is the maximum allowed age of a signed request.
const maxTimestampDrift = 5 * time.Second

// Authenticate returns HTTP middleware that validates the Authorization
// header (Bearer token), verifies the JWT access token, and stores the
// user address in the request context. Returns 401 for missing or invalid
// tokens.
func Authenticate(jwtMgr *JWTManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			address, ok := authenticateJWT(w, r, jwtMgr)
			if !ok {
				return
			}
			ctx := WithUserAddress(r.Context(), address)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Middleware returns HTTP middleware that authenticates requests using
// either HMAC-signed API keys (checked first) or JWT bearer tokens (fallback).
// On success it injects the authenticated user address into the request context.
func Middleware(jwtMgr *JWTManager, repo data.Repository, encryptionKey []byte, nonces *NonceTracker, logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Try API key auth first.
			if apiKey := r.Header.Get(HeaderAPIKey); apiKey != "" {
				address, ok := authenticateAPIKey(w, r, apiKey, repo, encryptionKey, nonces, logger)
				if !ok {
					return // response already written
				}
				ctx := WithUserAddress(r.Context(), address)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Fall back to JWT bearer token.
			if r.Header.Get("Authorization") != "" {
				address, ok := authenticateJWT(w, r, jwtMgr)
				if !ok {
					return
				}
				ctx := WithUserAddress(r.Context(), address)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			httputil.ErrorResponse(w, http.StatusUnauthorized, "missing credentials")
		})
	}
}

// authenticateJWT extracts and validates a Bearer token from the Authorization
// header. Returns the user address on success, or writes a 401 and returns false.
func authenticateJWT(w http.ResponseWriter, r *http.Request, jwtMgr *JWTManager) (string, bool) {
	header := r.Header.Get("Authorization")
	if header == "" {
		httputil.ErrorResponse(w, http.StatusUnauthorized, "missing authorization header")
		return "", false
	}

	parts := strings.SplitN(header, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		httputil.ErrorResponse(w, http.StatusUnauthorized, "invalid authorization header")
		return "", false
	}

	claims, err := jwtMgr.ValidateAccessToken(parts[1])
	if err != nil {
		httputil.ErrorResponse(w, http.StatusUnauthorized, "invalid token")
		return "", false
	}

	return claims.Subject, true
}

// authenticateAPIKey validates an HMAC-signed API key request. Returns the
// user address on success, or writes a 401 response and returns false.
func authenticateAPIKey(w http.ResponseWriter, r *http.Request, apiKey string, repo data.Repository, encryptionKey []byte, nonces *NonceTracker, logger *zap.Logger) (string, bool) {
	timestamp := r.Header.Get(HeaderTimestamp)
	signature := r.Header.Get(HeaderSignature)
	nonce := r.Header.Get(HeaderNonce)

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
		httputil.ErrorResponse(w, http.StatusUnauthorized, "timestamp drift too large")
		return "", false
	}

	// Check nonce replay.
	nonceKey := apiKey + ":" + nonce + ":" + timestamp
	if !nonces.Check(nonceKey) {
		httputil.ErrorResponse(w, http.StatusUnauthorized, "replayed request")
		return "", false
	}

	// Look up API key.
	keyHash := HashAPIKey(apiKey)
	stored, err := repo.GetAPIKeyByHash(r.Context(), keyHash)
	if err != nil {
		logger.Debug("api key lookup failed", zap.Error(err))
		httputil.ErrorResponse(w, http.StatusUnauthorized, "invalid api key")
		return "", false
	}

	// Decrypt HMAC secret and verify signature.
	secret, err := DecryptSecret(encryptionKey, stored.HMACSecretEncrypted)
	if err != nil {
		logger.Error("decrypting hmac secret", zap.Error(err))
		httputil.ErrorResponse(w, http.StatusUnauthorized, "invalid api key")
		return "", false
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, "failed to read request body")
		return "", false
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	if !VerifyHMACSignature(secret, timestamp, r.Method, r.URL.Path, string(body), signature) {
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
func VerifyHMACSignature(secret, timestamp, method, path, body, signature string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(BuildHMACMessage(timestamp, method, path, body)))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

// --- Nonce replay protection ---

// NonceTracker prevents replay of signed API key requests. It stores seen
// nonces in memory and periodically cleans up expired entries.
type NonceTracker struct {
	mu   sync.Mutex
	seen map[string]time.Time
	done chan struct{}
}

// NewNonceTracker creates a NonceTracker and starts background cleanup.
func NewNonceTracker() *NonceTracker {
	nt := &NonceTracker{
		seen: make(map[string]time.Time),
		done: make(chan struct{}),
	}
	go nt.cleanup()
	return nt
}

// Check returns true if the nonce has not been seen before, and records it.
// Returns false for replayed nonces.
func (nt *NonceTracker) Check(nonce string) bool {
	nt.mu.Lock()
	defer nt.mu.Unlock()
	if _, exists := nt.seen[nonce]; exists {
		return false
	}
	nt.seen[nonce] = time.Now()
	return true
}

// Stop shuts down the background cleanup goroutine.
func (nt *NonceTracker) Stop() {
	close(nt.done)
}

// cleanup periodically removes nonces older than twice the drift window.
func (nt *NonceTracker) cleanup() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-nt.done:
			return
		case <-ticker.C:
			nt.mu.Lock()
			cutoff := time.Now().Add(-2 * maxTimestampDrift)
			for k, t := range nt.seen {
				if t.Before(cutoff) {
					delete(nt.seen, k)
				}
			}
			nt.mu.Unlock()
		}
	}
}
