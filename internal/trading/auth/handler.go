package auth

import (
	"errors"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/Shisa-Fosho/services/internal/platform/data"
	"github.com/Shisa-Fosho/services/internal/shared/httputil"
)

// apiKeyTTL is how long a derived API key remains valid before requiring
// re-derivation. Polymarket clob-client re-derives on each session anyway;
// 30 days is a reasonable upper bound balancing UX and lease bounded
// compromise.
const apiKeyTTL = 30 * 24 * time.Hour

// Handler implements the trading service's Polymarket-compatible API-key
// lifecycle endpoints.
type Handler struct {
	logger        *zap.Logger
	repo          APIKeyRepository
	encKey        []byte
	apiKeyCfg     APIKeyConfig
	onAuthFailure func(*http.Request)
}

// HandlerOption configures an API-key Handler.
type HandlerOption func(*Handler)

// WithHandlerAuthFailureHook sets a callback invoked on EIP-712 sig
// verification failure inside /auth/derive-api-key. Used to drive rate-limiter
// lockout. Not invoked on shape errors (missing required headers).
func WithHandlerAuthFailureHook(hook func(*http.Request)) HandlerOption {
	return func(handler *Handler) { handler.onAuthFailure = hook }
}

// NewHandler creates an API-key handler.
func NewHandler(logger *zap.Logger, repo APIKeyRepository, apiKeyCfg APIKeyConfig, opts ...HandlerOption) *Handler {
	handler := &Handler{
		logger:    logger,
		repo:      repo,
		encKey:    apiKeyCfg.EncryptionKey,
		apiKeyCfg: apiKeyCfg,
	}
	for _, option := range opts {
		option(handler)
	}
	return handler
}

// RegisterRoutes registers the three API-key lifecycle endpoints on the mux.
// If authWrapper is non-nil, it is applied to all three routes — use it to
// inject a strict rate-limit profile. derive is additionally self-authenticated
// (EIP-712 signature); list/revoke are wrapped in AuthenticateAPIKey.
//
// Auth-wise:
//   - derive is L1-authenticated (EIP-712 wallet sig) — the handler verifies.
//   - list and revoke are L2-authenticated (HMAC) — wrapped in AuthenticateAPIKey.
func (handler *Handler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.Handler) http.Handler) {
	hmacOpts := []MiddlewareOption{}
	if handler.onAuthFailure != nil {
		hmacOpts = append(hmacOpts, WithAuthFailureHook(handler.onAuthFailure))
	}

	derive := wrap(authWrapper, http.HandlerFunc(handler.deriveAPIKey))
	list := wrap(authWrapper, AuthenticateAPIKey(handler.repo, handler.encKey, handler.logger, hmacOpts...)(http.HandlerFunc(handler.listAPIKeys)))
	revoke := wrap(authWrapper, AuthenticateAPIKey(handler.repo, handler.encKey, handler.logger, hmacOpts...)(http.HandlerFunc(handler.revokeAPIKey)))

	mux.Handle("GET /auth/derive-api-key", derive)
	mux.Handle("GET /auth/api-keys", list)
	mux.Handle("DELETE /auth/api-key", revoke)
}

// wrap applies mw to inner if mw is non-nil, otherwise returns inner unchanged.
func wrap(mw func(http.Handler) http.Handler, inner http.Handler) http.Handler {
	if mw == nil {
		return inner
	}
	return mw(inner)
}

func (handler *Handler) recordAuthFailure(r *http.Request) {
	if handler.onAuthFailure != nil {
		handler.onAuthFailure(r)
	}
}

// deriveAPIKeyResponse is the wire shape expected by clob-client v5.8.2's
// ApiKeyRaw type: {apiKey, secret, passphrase}. No expires_at — the SDK
// type doesn't model it.
type deriveAPIKeyResponse struct {
	APIKey     string `json:"apiKey"`
	Secret     string `json:"secret"`
	Passphrase string `json:"passphrase"`
}

// deriveAPIKey implements GET /auth/derive-api-key using L1 (EIP-712) headers.
// The request carries no body; credentials come from:
//
//	POLY_ADDRESS   — claimed wallet address
//	POLY_SIGNATURE — 65-byte EIP-712 signature over the ClobAuth message
//	POLY_TIMESTAMP — unix-seconds string signed into the ClobAuth message
//	POLY_NONCE     — EIP-712 nonce field (uint256, defaults to "0")
//
// The signature itself proves wallet control, so this endpoint is NOT wrapped
// in any middleware — anyone able to produce a valid EIP-712 sig for address X
// is, by definition, the owner of X.
func (handler *Handler) deriveAPIKey(w http.ResponseWriter, r *http.Request) {
	address := r.Header.Get(HeaderAddress)
	signature := r.Header.Get(HeaderSignature)
	timestamp := r.Header.Get(HeaderTimestamp)
	nonce := r.Header.Get(HeaderNonce)

	if address == "" || signature == "" || timestamp == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "POLY_ADDRESS, POLY_SIGNATURE, and POLY_TIMESTAMP headers are required")
		return
	}
	if nonce == "" {
		nonce = "0" // SDK default: createL1Headers sends "0" when nonce is unset.
	}

	sigBytes, err := VerifyEIP712Signature(address, timestamp, nonce, ClobAuthMessage, signature, handler.apiKeyCfg.ChainID)
	if err != nil {
		handler.logger.Info("EIP-712 verification failed", zap.String("address", address), zap.Error(err))
		handler.recordAuthFailure(r)
		httputil.ErrorResponse(w, http.StatusUnauthorized, "signature verification failed")
		return
	}

	apiKey, hmacSecret, passphrase := DeriveAPIKey(handler.apiKeyCfg.DerivationSecret, sigBytes)
	keyHash := HashAPIKey(apiKey)

	encryptedSecret, err := EncryptSecret(handler.apiKeyCfg.EncryptionKey, hmacSecret)
	if err != nil {
		handler.internalError(w, "encrypting HMAC secret", err)
		return
	}

	if err := handler.repo.UpsertAPIKey(r.Context(), &APIKey{
		KeyHash:             keyHash,
		UserAddress:         address,
		HMACSecretEncrypted: encryptedSecret,
		PassphraseHash:      HashAPIKey(passphrase),
		ExpiresAt:           time.Now().Add(apiKeyTTL),
	}); err != nil {
		handler.internalError(w, "upserting api key", err)
		return
	}

	_ = httputil.EncodeJSON(w, http.StatusOK, deriveAPIKeyResponse{
		APIKey:     apiKey,
		Secret:     hmacSecret,
		Passphrase: passphrase,
	})
}

// revokeAPIKey implements DELETE /auth/api-key. Auth is L2 HMAC; user address
// is extracted from context (set by AuthenticateAPIKey middleware). The
// request body carries the api_key whose hash we should mark revoked.
func (handler *Handler) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIKey string `json:"api_key"`
	}
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.APIKey == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "api_key is required")
		return
	}

	address := UserAddressFrom(r.Context())
	keyHash := HashAPIKey(req.APIKey)

	if err := handler.repo.RevokeAPIKey(r.Context(), keyHash, address); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.ErrorResponse(w, http.StatusNotFound, "api key not found")
			return
		}
		handler.internalError(w, "revoking api key", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// apiKeyListItem is the wire shape for listAPIKeys responses. HMAC secrets
// and passphrase hashes are never included.
type apiKeyListItem struct {
	KeyHash   string    `json:"key_hash"`
	Label     string    `json:"label"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// listAPIKeys implements GET /auth/api-keys. Auth is L2 HMAC; user address is
// extracted from context.
func (handler *Handler) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	address := UserAddressFrom(r.Context())

	keys, err := handler.repo.GetAPIKeysByUser(r.Context(), address)
	if err != nil {
		handler.internalError(w, "listing api keys", err)
		return
	}

	items := make([]apiKeyListItem, len(keys))
	for idx, key := range keys {
		items[idx] = apiKeyListItem{
			KeyHash:   key.KeyHash,
			Label:     key.Label,
			ExpiresAt: key.ExpiresAt,
			CreatedAt: key.CreatedAt,
		}
	}

	_ = httputil.EncodeJSON(w, http.StatusOK, items)
}

func (handler *Handler) internalError(w http.ResponseWriter, msg string, err error) {
	handler.logger.Error(msg, zap.Error(err))
	httputil.ErrorResponse(w, http.StatusInternalServerError, "internal server error")
}
