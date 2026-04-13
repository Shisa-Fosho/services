package auth

import (
	"net/http"
	"strings"

	"github.com/Shisa-Fosho/services/internal/platform/httputil"
)

// Authenticate returns HTTP middleware that validates the Authorization
// header (Bearer token), verifies the JWT access token, and stores the
// user address in the request context. Returns 401 for missing or invalid
// tokens.
func Authenticate(jwtMgr *JWTManager) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				httputil.ErrorResponse(w, http.StatusUnauthorized, "missing authorization header")
				return
			}

			parts := strings.SplitN(header, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				httputil.ErrorResponse(w, http.StatusUnauthorized, "invalid authorization header")
				return
			}

			claims, err := jwtMgr.ValidateAccessToken(parts[1])
			if err != nil {
				httputil.ErrorResponse(w, http.StatusUnauthorized, "invalid token")
				return
			}

			ctx := WithUserAddress(r.Context(), claims.Subject)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
