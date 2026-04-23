package market

import (
	"errors"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/Shisa-Fosho/services/internal/shared/httputil"
)

// Handler implements the platform service's market-domain HTTP endpoints.
type Handler struct {
	repo   Repository
	logger *zap.Logger
}

// NewHandler creates a new market handler.
func NewHandler(repo Repository, logger *zap.Logger) *Handler {
	return &Handler{repo: repo, logger: logger}
}

// RegisterAdminRoutes wires the admin-only category, event, and market
// endpoints onto mux. The provided adminMiddleware is expected to stack
// JWT authentication and the admin-wallet check — see platformauth.Authenticate
// composed with platformauth.RequireAdmin in cmd/platform/main.go.
func (handler *Handler) RegisterAdminRoutes(mux *http.ServeMux, adminMiddleware func(http.Handler) http.Handler) {
	mux.Handle("POST /admin/categories", adminMiddleware(http.HandlerFunc(handler.createCategory)))
	mux.Handle("PUT /admin/categories/{id}", adminMiddleware(http.HandlerFunc(handler.updateCategory)))
	mux.Handle("DELETE /admin/categories/{id}", adminMiddleware(http.HandlerFunc(handler.deleteCategory)))
	mux.Handle("PUT /admin/events/{id}", adminMiddleware(http.HandlerFunc(handler.updateEvent)))
	mux.Handle("PUT /admin/markets/{id}", adminMiddleware(http.HandlerFunc(handler.updateMarket)))
	mux.Handle("POST /admin/markets/{id}/pause", adminMiddleware(http.HandlerFunc(handler.pauseMarket)))
	mux.Handle("POST /admin/markets/{id}/resume", adminMiddleware(http.HandlerFunc(handler.resumeMarket)))
	mux.Handle("PUT /admin/markets/{id}/fee-rate", adminMiddleware(http.HandlerFunc(handler.setFeeRate)))
}

// categoryRequest is the shape both POST and PUT accept.
type categoryRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// categoryResponse is the JSON shape returned for a single category.
type categoryResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

func toCategoryResponse(cat *Category) categoryResponse {
	return categoryResponse{ID: cat.ID, Name: cat.Name, Slug: cat.Slug}
}

func (handler *Handler) createCategory(w http.ResponseWriter, r *http.Request) {
	var req categoryRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Name == "" || req.Slug == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "name and slug are required")
		return
	}

	cat := &Category{Name: req.Name, Slug: req.Slug}
	if err := handler.repo.CreateCategory(r.Context(), cat); err != nil {
		if errors.Is(err, ErrDuplicateSlug) {
			httputil.ErrorResponse(w, http.StatusConflict, "slug already in use")
			return
		}
		handler.internalError(w, "creating category", err)
		return
	}
	_ = httputil.EncodeJSON(w, http.StatusCreated, toCategoryResponse(cat))
}

func (handler *Handler) updateCategory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "id is required")
		return
	}

	var req categoryRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Name == "" || req.Slug == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "name and slug are required")
		return
	}

	updated, err := handler.repo.UpdateCategory(r.Context(), id, req.Name, req.Slug)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			httputil.ErrorResponse(w, http.StatusNotFound, "category not found")
		case errors.Is(err, ErrDuplicateSlug):
			httputil.ErrorResponse(w, http.StatusConflict, "slug already in use")
		default:
			handler.internalError(w, "updating category", err)
		}
		return
	}
	_ = httputil.EncodeJSON(w, http.StatusOK, toCategoryResponse(updated))
}

func (handler *Handler) deleteCategory(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "id is required")
		return
	}

	if err := handler.repo.DeleteCategory(r.Context(), id); err != nil {
		if errors.Is(err, ErrNotFound) {
			httputil.ErrorResponse(w, http.StatusNotFound, "category not found")
			return
		}
		handler.internalError(w, "deleting category", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// eventUpdateRequest is the PUT /admin/events/{id} body. All fields are
// optional; pointer semantics mean a missing field is unchanged. Category
// can be changed to another category but cannot be cleared — events must
// always belong to a category.
type eventUpdateRequest struct {
	Title             *string `json:"title,omitempty"`
	Description       *string `json:"description,omitempty"`
	CategoryID        *string `json:"category_id,omitempty"`
	Featured          *bool   `json:"featured,omitempty"`
	FeaturedSortOrder *int16  `json:"featured_sort_order,omitempty"`
}

// eventResponse is the JSON shape returned for a single event.
type eventResponse struct {
	ID                string `json:"id"`
	Slug              string `json:"slug"`
	Title             string `json:"title"`
	Description       string `json:"description"`
	CategoryID        string `json:"category_id"`
	EventType         string `json:"event_type"`
	Status            string `json:"status"`
	EndDate           string `json:"end_date"`
	Featured          bool   `json:"featured"`
	FeaturedSortOrder int16  `json:"featured_sort_order"`
}

func toEventResponse(event *Event) eventResponse {
	return eventResponse{
		ID:                event.ID,
		Slug:              event.Slug,
		Title:             event.Title,
		Description:       event.Description,
		CategoryID:        event.CategoryID,
		EventType:         event.EventType.String(),
		Status:            event.Status.String(),
		EndDate:           event.EndDate.UTC().Format(time.RFC3339),
		Featured:          event.Featured,
		FeaturedSortOrder: event.FeaturedSortOrder,
	}
}

func (handler *Handler) updateEvent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "id is required")
		return
	}

	var req eventUpdateRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	update := &EventUpdate{
		Title:             req.Title,
		Description:       req.Description,
		CategoryID:        req.CategoryID,
		Featured:          req.Featured,
		FeaturedSortOrder: req.FeaturedSortOrder,
	}
	updated, err := handler.repo.UpdateEvent(r.Context(), id, update)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidEvent):
			httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrNotFound):
			httputil.ErrorResponse(w, http.StatusNotFound, "event not found")
		default:
			handler.internalError(w, "updating event", err)
		}
		return
	}
	_ = httputil.EncodeJSON(w, http.StatusOK, toEventResponse(updated))
}

// marketUpdateRequest is the PUT /admin/markets/{id} body.
type marketUpdateRequest struct {
	Question        *string `json:"question,omitempty"`
	OutcomeYesLabel *string `json:"outcome_yes_label,omitempty"`
	OutcomeNoLabel  *string `json:"outcome_no_label,omitempty"`
}

// marketResponse is the JSON shape returned for a single market.
type marketResponse struct {
	ID              string  `json:"id"`
	Slug            string  `json:"slug"`
	EventID         *string `json:"event_id"`
	Question        string  `json:"question"`
	OutcomeYesLabel string  `json:"outcome_yes_label"`
	OutcomeNoLabel  string  `json:"outcome_no_label"`
	Status          string  `json:"status"`
	PriceYes        int64   `json:"price_yes"`
	PriceNo         int64   `json:"price_no"`
	Volume          int64   `json:"volume"`
	OpenInterest    int64   `json:"open_interest"`
}

func toMarketResponse(market *Market) marketResponse {
	return marketResponse{
		ID:              market.ID,
		Slug:            market.Slug,
		EventID:         market.EventID,
		Question:        market.Question,
		OutcomeYesLabel: market.OutcomeYesLabel,
		OutcomeNoLabel:  market.OutcomeNoLabel,
		Status:          market.Status.String(),
		PriceYes:        market.PriceYes,
		PriceNo:         market.PriceNo,
		Volume:          market.Volume,
		OpenInterest:    market.OpenInterest,
	}
}

func (handler *Handler) updateMarket(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "id is required")
		return
	}

	var req marketUpdateRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	update := &MarketUpdate{
		Question:        req.Question,
		OutcomeYesLabel: req.OutcomeYesLabel,
		OutcomeNoLabel:  req.OutcomeNoLabel,
	}
	updated, err := handler.repo.UpdateMarketMetadata(r.Context(), id, update)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidMarket):
			httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrNotFound):
			httputil.ErrorResponse(w, http.StatusNotFound, "market not found")
		default:
			handler.internalError(w, "updating market", err)
		}
		return
	}
	_ = httputil.EncodeJSON(w, http.StatusOK, toMarketResponse(updated))
}

func (handler *Handler) pauseMarket(w http.ResponseWriter, r *http.Request) {
	handler.transitionMarket(w, r, StatusPaused)
}

func (handler *Handler) resumeMarket(w http.ResponseWriter, r *http.Request) {
	handler.transitionMarket(w, r, StatusActive)
}

func (handler *Handler) transitionMarket(w http.ResponseWriter, r *http.Request, target Status) {
	id := r.PathValue("id")
	if id == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "id is required")
		return
	}

	updated, err := handler.repo.UpdateStatus(r.Context(), id, target)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			httputil.ErrorResponse(w, http.StatusNotFound, "market not found")
		case errors.Is(err, ErrInvalidTransition):
			httputil.ErrorResponse(w, http.StatusConflict, err.Error())
		default:
			handler.internalError(w, "transitioning market status", err)
		}
		return
	}
	_ = httputil.EncodeJSON(w, http.StatusOK, toMarketResponse(updated))
}

// feeRateRequest is the PUT /admin/markets/{id}/fee-rate body.
type feeRateRequest struct {
	FeeRateBps int `json:"fee_rate_bps"`
}

// feeRateResponse is the JSON shape returned for a market's fee rate.
type feeRateResponse struct {
	MarketID   string `json:"market_id"`
	FeeRateBps int    `json:"fee_rate_bps"`
	UpdatedAt  string `json:"updated_at"`
}

func toFeeRateResponse(rate *FeeRate) feeRateResponse {
	return feeRateResponse{
		MarketID:   rate.MarketID,
		FeeRateBps: rate.FeeRateBps,
		UpdatedAt:  rate.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func (handler *Handler) setFeeRate(w http.ResponseWriter, r *http.Request) {
	marketID := r.PathValue("id")
	if marketID == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "id is required")
		return
	}

	var req feeRateRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	rate := &FeeRate{MarketID: marketID, FeeRateBps: req.FeeRateBps}
	updated, err := handler.repo.UpsertFeeRate(r.Context(), rate)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidFeeRate):
			httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrNotFound):
			httputil.ErrorResponse(w, http.StatusNotFound, "market not found")
		default:
			handler.internalError(w, "upserting fee rate", err)
		}
		return
	}
	_ = httputil.EncodeJSON(w, http.StatusOK, toFeeRateResponse(updated))
}

func (handler *Handler) internalError(w http.ResponseWriter, msg string, err error) {
	handler.logger.Error(msg, zap.Error(err))
	httputil.ErrorResponse(w, http.StatusInternalServerError, "internal server error")
}
