package auth

import (
	"context"
	"net/http"

	"go.uber.org/zap"

	"github.com/Shisa-Fosho/services/internal/platform/data"
	"github.com/Shisa-Fosho/services/internal/shared/httputil"
)

// AuditAdminAction returns HTTP middleware that records a row in
// admin_audit_log for every request that passes through it AND produces a
// successful (2xx) response. Non-2xx responses are intentionally not
// recorded: 4xx/5xx bypass audit because those requests either failed
// authorization earlier in the chain (401/403 never reach this middleware)
// or failed business validation (4xx/5xx) without changing state.
//
// Expected chain composition (outermost first):
//
//	Authenticate → RequireAdmin → ratelimit("admin", KeyByUser) → AuditAdminAction → handler
//
// AuditAdminAction sits innermost so it observes the final response status.
// Write failures are logged and swallowed — the user-facing request is the
// source of truth, not the audit trail.
func AuditAdminAction(repo data.AdminAuditRepository, logger *zap.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			writer := httputil.NewStatusWriter(w)
			next.ServeHTTP(writer, r)

			if writer.Code < 200 || writer.Code >= 300 {
				return
			}

			adminAddress := AdminAddressFrom(r.Context())
			if adminAddress == "" {
				// RequireAdmin should always set this before we're reached;
				// missing admin implies a misconfigured middleware chain.
				logger.Warn("audit skipped: no admin address in context",
					zap.String("path", r.URL.Path),
				)
				return
			}

			action := &data.AdminAuditAction{
				AdminAddress: adminAddress,
				Method:       r.Method,
				Path:         r.URL.Path,
				Status:       writer.Code,
			}
			// Detach from the request context so a canceled request (client
			// disconnect after the handler returned 2xx) doesn't prevent the
			// audit write.
			if err := repo.RecordAdminAction(context.Background(), action); err != nil {
				logger.Error("recording admin audit entry",
					zap.String("admin_address", adminAddress),
					zap.String("method", r.Method),
					zap.String("path", r.URL.Path),
					zap.Int("status", writer.Code),
					zap.Error(err),
				)
			}
		})
	}
}
