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

	"github.com/Shisa-Fosho/services/internal/platform/data"
	"github.com/Shisa-Fosho/services/internal/shared/eth"
	"github.com/Shisa-Fosho/services/internal/shared/httputil"
)

const refreshCookieName = "refresh_token"

// Handler implements the platform service's session-auth REST endpoints.
type Handler struct {
	logger        *zap.Logger
	repo          data.SessionRepository
	jwt           *JWTManager
	siwe          MessageVerifier
	safeCfg       eth.SafeConfig
	secure        bool // Set Secure flag on cookies (true for non-localhost).
	onAuthFailure func(*http.Request)
}

// HandlerOption configures a session Handler.
type HandlerOption func(*Handler)

// WithHandlerAuthFailureHook sets a callback invoked on credential-verify
// failures inside session endpoints (SIWE sig mismatch, invalid refresh
// token). Used to drive rate-limiter lockout counters. Not invoked on shape
// errors (missing body, malformed JSON, empty required fields).
func WithHandlerAuthFailureHook(hook func(*http.Request)) HandlerOption {
	return func(handler *Handler) { handler.onAuthFailure = hook }
}

// NewHandler creates a new session handler.
func NewHandler(logger *zap.Logger, repo data.SessionRepository, jwt *JWTManager, siwe MessageVerifier, safeCfg eth.SafeConfig, secure bool, opts ...HandlerOption) *Handler {
	handler := &Handler{
		logger:  logger,
		repo:    repo,
		jwt:     jwt,
		siwe:    siwe,
		safeCfg: safeCfg,
		secure:  secure,
	}
	for _, option := range opts {
		option(handler)
	}
	return handler
}

// RegisterRoutes registers session-auth routes on the mux. If authWrapper is
// non-nil, it is applied to the credential-verify routes (signup, login,
// refresh) — use it to inject a strict rate-limit profile. nil disables wrapping.
func (handler *Handler) RegisterRoutes(mux *http.ServeMux, authWrapper func(http.Handler) http.Handler) {
	mux.HandleFunc("GET /auth/nonce", handler.nonce)
	mux.Handle("POST /auth/signup/wallet", wrap(authWrapper, http.HandlerFunc(handler.signupWallet)))
	mux.Handle("POST /auth/login/wallet", wrap(authWrapper, http.HandlerFunc(handler.loginWallet)))
	mux.Handle("POST /auth/refresh", wrap(authWrapper, http.HandlerFunc(handler.refresh)))
	mux.HandleFunc("POST /auth/logout", handler.logout)
	mux.Handle("GET /auth/session", Authenticate(handler.jwt)(http.HandlerFunc(handler.session)))
}

// wrap applies mw to next if mw is non-nil, otherwise returns next unchanged.
func wrap(mw func(http.Handler) http.Handler, next http.Handler) http.Handler {
	if mw == nil {
		return next
	}
	return mw(next)
}

func (handler *Handler) recordAuthFailure(r *http.Request) {
	if handler.onAuthFailure != nil {
		handler.onAuthFailure(r)
	}
}

type nonceResponse struct {
	Nonce string `json:"nonce"`
}

func (handler *Handler) nonce(w http.ResponseWriter, _ *http.Request) {
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

func (handler *Handler) signupWallet(w http.ResponseWriter, r *http.Request) {
	var req walletSignupRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Message == "" || req.Signature == "" || req.Username == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "message, signature, and username are required")
		return
	}

	address, err := handler.siwe.Verify(req.Message, req.Signature)
	if err != nil {
		handler.logger.Info("SIWE verification failed", zap.Error(err))
		handler.recordAuthFailure(r)
		httputil.ErrorResponse(w, http.StatusUnauthorized, "signature verification failed")
		return
	}

	safeAddr := eth.DeriveSafeAddress(handler.safeCfg, common.HexToAddress(address))

	user := &data.User{
		Address:      address,
		Username:     req.Username,
		SignupMethod: data.SignupMethodWallet,
		SafeAddress:  safeAddr.Hex(),
	}
	if err := handler.repo.CreateUser(r.Context(), user); err != nil {
		if errors.Is(err, data.ErrDuplicateUser) {
			httputil.ErrorResponse(w, http.StatusConflict, "user already exists")
			return
		}
		if errors.Is(err, data.ErrInvalidUser) {
			httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
			return
		}
		handler.internalError(w, "creating user", err)
		return
	}

	handler.issueSession(w, r, address, safeAddr.Hex())
}

type walletLoginRequest struct {
	Message   string `json:"message"`
	Signature string `json:"signature"`
}

func (handler *Handler) loginWallet(w http.ResponseWriter, r *http.Request) {
	var req walletLoginRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Message == "" || req.Signature == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "message and signature are required")
		return
	}

	address, err := handler.siwe.Verify(req.Message, req.Signature)
	if err != nil {
		handler.logger.Info("SIWE verification failed", zap.Error(err))
		handler.recordAuthFailure(r)
		httputil.ErrorResponse(w, http.StatusUnauthorized, "signature verification failed")
		return
	}

	user, err := handler.repo.GetUserByAddress(r.Context(), address)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			handler.recordAuthFailure(r)
			httputil.ErrorResponse(w, http.StatusUnauthorized, "user not found")
			return
		}
		handler.internalError(w, "getting user", err)
		return
	}

	handler.issueSession(w, r, user.Address, user.SafeAddress)
}

func (handler *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(refreshCookieName)
	if err != nil {
		httputil.ErrorResponse(w, http.StatusUnauthorized, "missing refresh token")
		return
	}

	claims, err := handler.jwt.ValidateRefreshToken(cookie.Value)
	if err != nil {
		handler.recordAuthFailure(r)
		httputil.ErrorResponse(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}

	stored, err := handler.repo.GetRefreshToken(r.Context(), claims.ID)
	if err != nil {
		handler.recordAuthFailure(r)
		httputil.ErrorResponse(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	if stored.Revoked {
		handler.recordAuthFailure(r)
		httputil.ErrorResponse(w, http.StatusUnauthorized, "token revoked")
		return
	}

	if err := handler.repo.RevokeRefreshToken(r.Context(), claims.ID); err != nil {
		handler.internalError(w, "revoking old refresh token", err)
		return
	}

	accessToken, err := handler.jwt.IssueAccessToken(claims.Subject)
	if err != nil {
		handler.internalError(w, "issuing access token", err)
		return
	}

	refreshToken, jti, expiresAt, err := handler.jwt.IssueRefreshToken(claims.Subject)
	if err != nil {
		handler.internalError(w, "issuing refresh token", err)
		return
	}

	if err := handler.repo.StoreRefreshToken(r.Context(), &data.RefreshToken{
		ID:          jti,
		UserAddress: claims.Subject,
		ExpiresAt:   expiresAt,
	}); err != nil {
		handler.internalError(w, "storing refresh token", err)
		return
	}

	handler.setRefreshCookie(w, refreshToken, expiresAt)
	_ = httputil.EncodeJSON(w, http.StatusOK, map[string]string{
		"access_token": accessToken,
	})
}

func (handler *Handler) logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(refreshCookieName)
	if err == nil && cookie.Value != "" {
		claims, err := handler.jwt.ValidateRefreshToken(cookie.Value)
		if err == nil {
			_ = handler.repo.RevokeRefreshToken(r.Context(), claims.ID)
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    "",
		Path:     "/auth",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   handler.secure,
	})
	w.WriteHeader(http.StatusNoContent)
}

type sessionResponse struct {
	Address     string `json:"address"`
	Username    string `json:"username"`
	SafeAddress string `json:"safe_address"`
}

func (handler *Handler) session(w http.ResponseWriter, r *http.Request) {
	address := UserAddressFrom(r.Context())
	user, err := handler.repo.GetUserByAddress(r.Context(), address)
	if err != nil {
		if errors.Is(err, data.ErrNotFound) {
			httputil.ErrorResponse(w, http.StatusUnauthorized, "user not found")
			return
		}
		handler.internalError(w, "getting user for session", err)
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
func (handler *Handler) issueSession(w http.ResponseWriter, r *http.Request, address, safeAddress string) {
	accessToken, err := handler.jwt.IssueAccessToken(address)
	if err != nil {
		handler.internalError(w, "issuing access token", err)
		return
	}

	refreshToken, jti, expiresAt, err := handler.jwt.IssueRefreshToken(address)
	if err != nil {
		handler.internalError(w, "issuing refresh token", err)
		return
	}

	if err := handler.repo.StoreRefreshToken(r.Context(), &data.RefreshToken{
		ID:          jti,
		UserAddress: address,
		ExpiresAt:   expiresAt,
	}); err != nil {
		handler.internalError(w, "storing refresh token", err)
		return
	}

	handler.setRefreshCookie(w, refreshToken, expiresAt)
	_ = httputil.EncodeJSON(w, http.StatusOK, authResponse{
		AccessToken: accessToken,
		Address:     address,
		SafeAddress: safeAddress,
	})
}

func (handler *Handler) internalError(w http.ResponseWriter, msg string, err error) {
	handler.logger.Error(msg, zap.Error(err))
	httputil.ErrorResponse(w, http.StatusInternalServerError, "internal server error")
}

func (handler *Handler) setRefreshCookie(w http.ResponseWriter, token string, expiresAt time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     refreshCookieName,
		Value:    token,
		Path:     "/auth",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   handler.secure,
	})
}
