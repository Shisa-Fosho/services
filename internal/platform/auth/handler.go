package auth

import (
	"errors"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Shisa-Fosho/services/internal/data"
	"github.com/Shisa-Fosho/services/internal/platform/eth"
	"github.com/Shisa-Fosho/services/internal/platform/httputil"
)

const refreshCookieName = "refresh_token"

// Handler implements the auth REST endpoints.
type Handler struct {
	logger    *zap.Logger
	repo      data.Repository
	jwt       *JWTManager
	siwe      MessageVerifier
	safeCfg   eth.SafeConfig
	secure    bool // Set Secure flag on cookies (true for non-localhost).
	apiKeyCfg APIKeyConfig
}

// NewHandler creates a new auth handler with all dependencies.
func NewHandler(logger *zap.Logger, repo data.Repository, jwt *JWTManager, siwe MessageVerifier, safeCfg eth.SafeConfig, secure bool, apiKeyCfg APIKeyConfig) *Handler {
	return &Handler{
		logger:    logger,
		repo:      repo,
		jwt:       jwt,
		siwe:      siwe,
		safeCfg:   safeCfg,
		secure:    secure,
		apiKeyCfg: apiKeyCfg,
	}
}

// RegisterRoutes registers auth routes on the given mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/nonce", h.nonce)
	mux.HandleFunc("POST /auth/signup/wallet", h.signupWallet)
	mux.HandleFunc("POST /auth/login/wallet", h.loginWallet)
	mux.HandleFunc("POST /auth/refresh", h.refresh)
	mux.HandleFunc("POST /auth/logout", h.logout)
	mux.Handle("GET /auth/session", Authenticate(h.jwt)(http.HandlerFunc(h.session)))
	mux.Handle("POST /auth/derive-api-key", Authenticate(h.jwt)(http.HandlerFunc(h.deriveAPIKey)))
	mux.Handle("DELETE /auth/api-key", Authenticate(h.jwt)(http.HandlerFunc(h.revokeAPIKey)))
	mux.Handle("GET /auth/api-keys", Authenticate(h.jwt)(http.HandlerFunc(h.listAPIKeys)))
}

type nonceResponse struct {
	Nonce string `json:"nonce"`
}

func (h *Handler) nonce(w http.ResponseWriter, _ *http.Request) {
	_ = httputil.EncodeJSON(w, http.StatusOK, nonceResponse{Nonce: GenerateNonce()})
}

type walletSignupRequest struct {
	Message   string `json:"message"`
	Signature string `json:"signature"`
	Username  string `json:"username"`
}

type authResponse struct {
	AccessToken string `json:"access_token"`
	Address     string `json:"address"`
	SafeAddress string `json:"safe_address"`
}

func (h *Handler) signupWallet(w http.ResponseWriter, r *http.Request) {
	var req walletSignupRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Message == "" || req.Signature == "" || req.Username == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "message, signature, and username are required")
		return
	}

	// Verify SIWE signature.
	address, err := h.siwe.Verify(req.Message, req.Signature)
	if err != nil {
		h.logger.Info("SIWE verification failed", zap.Error(err))
		httputil.ErrorResponse(w, http.StatusUnauthorized, "signature verification failed")
		return
	}

	// Derive Safe address.
	safeAddr := eth.DeriveSafeAddress(h.safeCfg, common.HexToAddress(address))

	// Create user.
	user := &data.User{
		Address:      address,
		Username:     req.Username,
		SignupMethod: data.SignupMethodWallet,
		SafeAddress:  safeAddr.Hex(),
	}
	if err := h.repo.CreateUser(r.Context(), user); err != nil {
		if errors.Is(err, data.ErrDuplicateUser) {
			httputil.ErrorResponse(w, http.StatusConflict, "user already exists")
			return
		}
		if errors.Is(err, data.ErrInvalidUser) {
			httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
			return
		}
		h.internalError(w, "creating user", err)
		return
	}

	h.issueSession(w, r, address, safeAddr.Hex())
}

type walletLoginRequest struct {
	Message   string `json:"message"`
	Signature string `json:"signature"`
}

func (h *Handler) loginWallet(w http.ResponseWriter, r *http.Request) {
	var req walletLoginRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Message == "" || req.Signature == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "message and signature are required")
		return
	}

	address, err := h.siwe.Verify(req.Message, req.Signature)
	if err != nil {
		h.logger.Info("SIWE verification failed", zap.Error(err))
		httputil.ErrorResponse(w, http.StatusUnauthorized, "signature verification failed")
		return
	}

	user, err := h.repo.GetUserByAddress(r.Context(), address)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.ErrorResponse(w, http.StatusUnauthorized, "user not found")
			return
		}
		h.internalError(w, "getting user", err)
		return
	}

	h.issueSession(w, r, user.Address, user.SafeAddress)
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(refreshCookieName)
	if err != nil {
		httputil.ErrorResponse(w, http.StatusUnauthorized, "missing refresh token")
		return
	}

	claims, err := h.jwt.ValidateRefreshToken(cookie.Value)
	if err != nil {
		httputil.ErrorResponse(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}

	// Check token in database.
	stored, err := h.repo.GetRefreshToken(r.Context(), claims.ID)
	if err != nil {
		httputil.ErrorResponse(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	if stored.Revoked {
		httputil.ErrorResponse(w, http.StatusUnauthorized, "token revoked")
		return
	}

	// Revoke the old token (rotation).
	if err := h.repo.RevokeRefreshToken(r.Context(), claims.ID); err != nil {
		h.internalError(w, "revoking old refresh token", err)
		return
	}

	// Issue new tokens.
	accessToken, err := h.jwt.IssueAccessToken(claims.Subject)
	if err != nil {
		h.internalError(w, "issuing access token", err)
		return
	}

	refreshToken, jti, expiresAt, err := h.jwt.IssueRefreshToken(claims.Subject)
	if err != nil {
		h.internalError(w, "issuing refresh token", err)
		return
	}

	if err := h.repo.StoreRefreshToken(r.Context(), &data.RefreshToken{
		ID:          jti,
		UserAddress: claims.Subject,
		ExpiresAt:   expiresAt,
	}); err != nil {
		h.internalError(w, "storing refresh token", err)
		return
	}

	h.setRefreshCookie(w, refreshToken, expiresAt)
	_ = httputil.EncodeJSON(w, http.StatusOK, map[string]string{
		"access_token": accessToken,
	})
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(refreshCookieName)
	if err == nil && cookie.Value != "" {
		claims, err := h.jwt.ValidateRefreshToken(cookie.Value)
		if err == nil {
			_ = h.repo.RevokeRefreshToken(r.Context(), claims.ID)
		}
	}

	// Clear cookie regardless.
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     "/auth",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   h.secure,
	})
	w.WriteHeader(http.StatusNoContent)
}

type sessionResponse struct {
	Address     string `json:"address"`
	Username    string `json:"username"`
	SafeAddress string `json:"safe_address"`
}

func (h *Handler) session(w http.ResponseWriter, r *http.Request) {
	address := UserAddressFrom(r.Context())
	user, err := h.repo.GetUserByAddress(r.Context(), address)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.ErrorResponse(w, http.StatusUnauthorized, "user not found")
			return
		}
		h.internalError(w, "getting user for session", err)
		return
	}

	_ = httputil.EncodeJSON(w, http.StatusOK, sessionResponse{
		Address:     user.Address,
		Username:    user.Username,
		SafeAddress: user.SafeAddress,
	})
}

// API key types and handlers.

const apiKeyTTL = 30 * 24 * time.Hour // 30 days.

type deriveAPIKeyRequest struct {
	Signature string `json:"signature"`
	Timestamp string `json:"timestamp"`
	Nonce     string `json:"nonce"`
}

type deriveAPIKeyResponse struct {
	APIKey     string    `json:"api_key"`
	HMACSecret string    `json:"hmac_secret"`
	ExpiresAt  time.Time `json:"expires_at"`
}

func (h *Handler) deriveAPIKey(w http.ResponseWriter, r *http.Request) {
	var req deriveAPIKeyRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Signature == "" || req.Timestamp == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "signature and timestamp are required")
		return
	}
	if req.Nonce == "" {
		req.Nonce = "0"
	}

	address := UserAddressFrom(r.Context())

	sigBytes, err := VerifyEIP712Signature(address, req.Timestamp, req.Nonce, ClobAuthMessage, req.Signature, h.apiKeyCfg.ChainID)
	if err != nil {
		h.logger.Info("EIP-712 verification failed", zap.String("address", address), zap.Error(err))
		httputil.ErrorResponse(w, http.StatusUnauthorized, "signature verification failed")
		return
	}

	apiKey, hmacSecret := DeriveAPIKey(h.apiKeyCfg.DerivationSecret, sigBytes)
	keyHash := HashAPIKey(apiKey)

	encryptedSecret, err := EncryptSecret(h.apiKeyCfg.EncryptionKey, hmacSecret)
	if err != nil {
		h.internalError(w, "encrypting HMAC secret", err)
		return
	}

	expiresAt := time.Now().Add(apiKeyTTL)
	if err := h.repo.UpsertAPIKey(r.Context(), &data.APIKey{
		KeyHash:             keyHash,
		UserAddress:         address,
		HMACSecretEncrypted: encryptedSecret,
		ExpiresAt:           expiresAt,
	}); err != nil {
		h.internalError(w, "upserting api key", err)
		return
	}

	_ = httputil.EncodeJSON(w, http.StatusOK, deriveAPIKeyResponse{
		APIKey:     apiKey,
		HMACSecret: hmacSecret,
		ExpiresAt:  expiresAt,
	})
}

type revokeAPIKeyRequest struct {
	APIKey string `json:"api_key"`
}

func (h *Handler) revokeAPIKey(w http.ResponseWriter, r *http.Request) {
	var req revokeAPIKeyRequest
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

type apiKeyListItem struct {
	KeyHash   string    `json:"key_hash"`
	Label     string    `json:"label"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

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

// issueSession creates access + refresh tokens, stores the refresh token,
// sets the cookie, and writes the JSON response.
func (h *Handler) issueSession(w http.ResponseWriter, r *http.Request, address, safeAddress string) {
	accessToken, err := h.jwt.IssueAccessToken(address)
	if err != nil {
		h.internalError(w, "issuing access token", err)
		return
	}

	refreshToken, jti, expiresAt, err := h.jwt.IssueRefreshToken(address)
	if err != nil {
		h.internalError(w, "issuing refresh token", err)
		return
	}

	if err := h.repo.StoreRefreshToken(r.Context(), &data.RefreshToken{
		ID:          jti,
		UserAddress: address,
		ExpiresAt:   expiresAt,
	}); err != nil {
		h.internalError(w, "storing refresh token", err)
		return
	}

	h.setRefreshCookie(w, refreshToken, expiresAt)
	_ = httputil.EncodeJSON(w, http.StatusOK, authResponse{
		AccessToken: accessToken,
		Address:     address,
		SafeAddress: safeAddress,
	})
}

func (h *Handler) internalError(w http.ResponseWriter, msg string, err error) {
	h.logger.Error(msg, zap.Error(err))
	httputil.ErrorResponse(w, http.StatusInternalServerError, "internal server error")
}

func (h *Handler) setRefreshCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    token,
		Path:     "/auth",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   h.secure,
	})
}
