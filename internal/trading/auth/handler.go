package auth

import (
	"errors"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/Shisa-Fosho/services/internal/data"
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
	logger    *zap.Logger
	repo      APIKeyRepository
	encKey    []byte
	apiKeyCfg APIKeyConfig
}

// NewHandler creates an API-key handler.
func NewHandler(logger *zap.Logger, repo APIKeyRepository, apiKeyCfg APIKeyConfig) *Handler {
	return &Handler{
		logger:    logger,
		repo:      repo,
		encKey:    apiKeyCfg.EncryptionKey,
		apiKeyCfg: apiKeyCfg,
	}
}

// RegisterRoutes registers the three API-key lifecycle endpoints on the mux.
//
// Auth-wise:
//   - derive is L1-authenticated (EIP-712 wallet sig in POLY_* headers) —
//     no middleware wrapper; the handler verifies the signature itself.
//   - list and revoke are L2-authenticated (HMAC over request with existing
//     API-key creds) — wrapped in AuthenticateAPIKey middleware.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/derive-api-key", h.deriveAPIKey)
	mux.Handle("GET /auth/api-keys",
		AuthenticateAPIKey(h.repo, h.encKey, h.logger)(http.HandlerFunc(h.listAPIKeys)))
	mux.Handle("DELETE /auth/api-key",
		AuthenticateAPIKey(h.repo, h.encKey, h.logger)(http.HandlerFunc(h.revokeAPIKey)))
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
func (h *Handler) deriveAPIKey(w http.ResponseWriter, r *http.Request) {
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

	sigBytes, err := VerifyEIP712Signature(address, timestamp, nonce, ClobAuthMessage, signature, h.apiKeyCfg.ChainID)
	if err != nil {
		h.logger.Info("EIP-712 verification failed", zap.String("address", address), zap.Error(err))
		httputil.ErrorResponse(w, http.StatusUnauthorized, "signature verification failed")
		return
	}

	apiKey, hmacSecret, passphrase := DeriveAPIKey(h.apiKeyCfg.DerivationSecret, sigBytes)
	keyHash := HashAPIKey(apiKey)

	encryptedSecret, err := EncryptSecret(h.apiKeyCfg.EncryptionKey, hmacSecret)
	if err != nil {
		h.internalError(w, "encrypting HMAC secret", err)
		return
	}

	if err := h.repo.UpsertAPIKey(r.Context(), &APIKey{
		KeyHash:             keyHash,
		UserAddress:         address,
		HMACSecretEncrypted: encryptedSecret,
		PassphraseHash:      HashAPIKey(passphrase),
		ExpiresAt:           time.Now().Add(apiKeyTTL),
	}); err != nil {
		h.internalError(w, "upserting api key", err)
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
func (h *Handler) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
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

	if err := h.repo.RevokeAPIKey(r.Context(), keyHash, address); err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.ErrorResponse(w, http.StatusNotFound, "api key not found")
			return
		}
		h.internalError(w, "revoking api key", err)
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
func (h *Handler) listAPIKeys(w http.ResponseWriter, r *http.Request) {
	address := UserAddressFrom(r.Context())

	keys, err := h.repo.GetAPIKeysByUser(r.Context(), address)
	if err != nil {
		h.internalError(w, "listing api keys", err)
		return
	}

	items := make([]apiKeyListItem, len(keys))
	for i, k := range keys {
		items[i] = apiKeyListItem{
			KeyHash:   k.KeyHash,
			Label:     k.Label,
			ExpiresAt: k.ExpiresAt,
			CreatedAt: k.CreatedAt,
		}
	}

	_ = httputil.EncodeJSON(w, http.StatusOK, items)
}

func (h *Handler) internalError(w http.ResponseWriter, msg string, err error) {
	h.logger.Error(msg, zap.Error(err))
	httputil.ErrorResponse(w, http.StatusInternalServerError, "internal server error")
}
