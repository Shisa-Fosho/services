package market

import (
	"context"
	"net/http"

	"go.uber.org/zap"

	"github.com/Shisa-Fosho/services/internal/shared/eth"
	"github.com/Shisa-Fosho/services/internal/shared/httputil"
)

// configStore is the market-config bucket surface the handler depends on:
// publishes for writes, LiveStatus for the read overlay on admin GETs.
// The concrete *Publisher in this package satisfies it; tests inject stubs.
type configStore interface {
	PublishMarketConfig(market *Market) error
	PublishStatusChange(ctx context.Context, marketID string, status Status) error
	PublishStatusChangeWithOutcome(ctx context.Context, marketID string, status Status, outcome *Outcome) error
	LiveStatus(marketID string) (string, bool, error)
}

// Handler implements the platform service's market-domain HTTP endpoints.
// The handlers themselves are split across handler_categories.go,
// handler_markets.go, handler_events.go, handler_events_create.go, and
// handler_events_resolve.go. This file owns the constructor, the route
// table, and the cross-resource error helpers.
type Handler struct {
	repo      Repository
	publisher configStore
	ct        eth.CTReader
	negRisk   eth.NegRiskReader
	logger    *zap.Logger
}

// NewHandler creates a new market handler. The publisher writes config
// updates to NATS KV (consumed by trading) and publishes status changes
// on Core NATS (consumed by the WebSocket server). The on-chain readers
// perform verification on create / resolve / void; they are separate
// interfaces because each contract is independently versioned and
// independently deployable (NegRisk is optional).
func NewHandler(repo Repository, publisher configStore, ct eth.CTReader, negRisk eth.NegRiskReader, logger *zap.Logger) *Handler {
	return &Handler{repo: repo, publisher: publisher, ct: ct, negRisk: negRisk, logger: logger}
}

// RegisterAdminRoutes wires the admin-only category, event, and market
// endpoints onto mux. The provided adminMiddleware is expected to stack
// JWT authentication and the admin-wallet check — see platformauth.Authenticate
// composed with platformauth.RequireAdmin in cmd/platform/main.go.
//
// Event create + resolve are split per market type so each request shape
// is unambiguous: the URL is the discriminator, not an in-body event_type
// field. Void is binary-only (NegRisk void is deferred).
func (handler *Handler) RegisterAdminRoutes(mux *http.ServeMux, adminMiddleware func(http.Handler) http.Handler) {
	mux.Handle("POST /admin/categories", adminMiddleware(http.HandlerFunc(handler.createCategory)))
	mux.Handle("PUT /admin/categories/{id}", adminMiddleware(http.HandlerFunc(handler.updateCategory)))
	mux.Handle("DELETE /admin/categories/{id}", adminMiddleware(http.HandlerFunc(handler.deleteCategory)))

	mux.Handle("POST /admin/events/binary", adminMiddleware(http.HandlerFunc(handler.createBinaryEvent)))
	mux.Handle("POST /admin/events/neg-risk", adminMiddleware(http.HandlerFunc(handler.createNegRiskEvent)))
	mux.Handle("PUT /admin/events/{id}", adminMiddleware(http.HandlerFunc(handler.updateEvent)))
	mux.Handle("POST /admin/events/{id}/binary/resolve", adminMiddleware(http.HandlerFunc(handler.resolveBinaryEvent)))
	mux.Handle("POST /admin/events/{id}/neg-risk/resolve", adminMiddleware(http.HandlerFunc(handler.resolveNegRiskEvent)))
	mux.Handle("POST /admin/events/{id}/void", adminMiddleware(http.HandlerFunc(handler.voidEvent)))

	mux.Handle("POST /admin/markets/bulk-pause", adminMiddleware(http.HandlerFunc(handler.bulkPauseMarkets)))
	mux.Handle("GET /admin/markets/{id}", adminMiddleware(http.HandlerFunc(handler.getMarket)))
	mux.Handle("PUT /admin/markets/{id}", adminMiddleware(http.HandlerFunc(handler.updateMarket)))
	mux.Handle("POST /admin/markets/{id}/pause", adminMiddleware(http.HandlerFunc(handler.pauseMarket)))
	mux.Handle("POST /admin/markets/{id}/resume", adminMiddleware(http.HandlerFunc(handler.resumeMarket)))
	mux.Handle("PUT /admin/markets/{id}/fee-rate", adminMiddleware(http.HandlerFunc(handler.setFeeRate)))
	mux.Handle("PUT /admin/markets/{id}/trading-config", adminMiddleware(http.HandlerFunc(handler.setTradingConfig)))
}

func (handler *Handler) internalError(w http.ResponseWriter, msg string, err error) {
	handler.logger.Error(msg, zap.Error(err))
	httputil.ErrorResponse(w, http.StatusInternalServerError, "internal server error")
}

// chainError reports a chain-RPC failure as a 502. Distinct from
// publishFailed so the user-facing message is accurate.
func (handler *Handler) chainError(w http.ResponseWriter, op string, err error) {
	handler.logger.Error("chain read failed", zap.String("op", op), zap.Error(err))
	httputil.ErrorResponse(w, http.StatusBadGateway,
		"on-chain read failed; please retry")
}

// publishFailed logs a downstream-publish failure and writes a 502 response.
// Used when the DB commit succeeded but a follow-on NATS publish failed —
// caller must return after invoking.
func (handler *Handler) publishFailed(w http.ResponseWriter, op, userMsg, marketID string, err error, extra ...zap.Field) {
	fields := append([]zap.Field{zap.String("market_id", marketID), zap.Error(err)}, extra...)
	handler.logger.Error("publishing "+op+" failed", fields...)
	httputil.ErrorResponse(w, http.StatusBadGateway, userMsg)
}
