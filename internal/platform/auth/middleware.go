package auth

import (
	"net/http"
	"strings"

	"github.com/Shisa-Fosho/services/internal/shared/httputil"
)

// Authenticate returns HTTP middleware that validates the Authorization header
// (Bearer token), verifies the JWT access token, and stores the user address
// in the request context. Used by platform-service-owned session endpoints
// (profile, session info, management UI). Returns 401 for missing or invalid
// tokens. This middleware is JWT-only; it does not accept POLY_* headers.
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
