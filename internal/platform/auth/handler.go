// Package auth implements the platform service's session-auth HTTP endpoints
// (SIWE signup/login, JWT refresh, logout, session info) plus the underlying
// JWT and SIWE primitives and the JWT-only Authenticate middleware.
//
// API-key lifecycle (derive, list, revoke) and HMAC verification live in the
// trading service at internal/trading/auth. This split matches Polymarket's
// architectural division: gamma-api handles session, clob handles API keys.
package auth

import (
	"errors"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Shisa-Fosho/services/internal/data"
	"github.com/Shisa-Fosho/services/internal/shared/eth"
	"github.com/Shisa-Fosho/services/internal/shared/httputil"
)

const refreshCookieName = "refresh_token"

// Handler implements the platform service's session-auth REST endpoints.
type Handler struct {
	logger  *zap.Logger
	repo    data.SessionRepository
	jwt     *JWTManager
	siwe    MessageVerifier
	safeCfg eth.SafeConfig
	secure  bool // Set Secure flag on cookies (true for non-localhost).
}

// NewHandler creates a new session handler.
func NewHandler(logger *zap.Logger, repo data.SessionRepository, jwt *JWTManager, siwe MessageVerifier, safeCfg eth.SafeConfig, secure bool) *Handler {
	return &Handler{
		logger:  logger,
		repo:    repo,
		jwt:     jwt,
		siwe:    siwe,
		safeCfg: safeCfg,
		secure:  secure,
	}
}

// RegisterRoutes registers session-auth routes on the mux.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/nonce", h.nonce)
	mux.HandleFunc("POST /auth/signup/wallet", h.signupWallet)
	mux.HandleFunc("POST /auth/login/wallet", h.loginWallet)
	mux.HandleFunc("POST /auth/refresh", h.refresh)
	mux.HandleFunc("POST /auth/logout", h.logout)
	mux.Handle("GET /auth/session", Authenticate(h.jwt)(http.HandlerFunc(h.session)))
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

	address, err := h.siwe.Verify(req.Message, req.Signature)
	if err != nil {
		h.logger.Info("SIWE verification failed", zap.Error(err))
		httputil.ErrorResponse(w, http.StatusUnauthorized, "signature verification failed")
		return
	}

	safeAddr := eth.DeriveSafeAddress(h.safeCfg, common.HexToAddress(address))

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

	stored, err := h.repo.GetRefreshToken(r.Context(), claims.ID)
	if err != nil {
		httputil.ErrorResponse(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	if stored.Revoked {
		httputil.ErrorResponse(w, http.StatusUnauthorized, "token revoked")
		return
	}

	if err := h.repo.RevokeRefreshToken(r.Context(), claims.ID); err != nil {
		h.internalError(w, "revoking old refresh token", err)
		return
	}

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
