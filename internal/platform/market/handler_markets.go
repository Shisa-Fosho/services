package market

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/Shisa-Fosho/services/internal/shared/httputil"
)

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
	EventID         string  `json:"event_id"`
	Question        string  `json:"question"`
	OutcomeYesLabel string  `json:"outcome_yes_label"`
	OutcomeNoLabel  string  `json:"outcome_no_label"`
	TokenIDYes      string  `json:"token_id_yes"`
	TokenIDNo       string  `json:"token_id_no"`
	ConditionID     string  `json:"condition_id"`
	QuestionID      string  `json:"question_id"`
	Status          string  `json:"status"`
	Outcome         *string `json:"outcome,omitempty"`
	PriceYes        int64   `json:"price_yes"`
	PriceNo         int64   `json:"price_no"`
	Volume          int64   `json:"volume"`
	OpenInterest    int64   `json:"open_interest"`
	FeeRateBps      *int64  `json:"fee_rate_bps,omitempty"`
	TickSize        string  `json:"tick_size"`
	MinSize         int64   `json:"min_size"`
	MaxSize         *int64  `json:"max_size,omitempty"`
}

func toMarketResponse(market *Market) marketResponse {
	out := marketResponse{
		ID:              market.ID,
		Slug:            market.Slug,
		EventID:         market.EventID,
		Question:        market.Question,
		OutcomeYesLabel: market.OutcomeYesLabel,
		OutcomeNoLabel:  market.OutcomeNoLabel,
		TokenIDYes:      market.TokenIDYes,
		TokenIDNo:       market.TokenIDNo,
		ConditionID:     market.ConditionID,
		QuestionID:      market.QuestionID,
		Status:          market.Status.String(),
		PriceYes:        market.PriceYes,
		PriceNo:         market.PriceNo,
		Volume:          market.Volume,
		OpenInterest:    market.OpenInterest,
		FeeRateBps:      market.FeeRateBps,
		TickSize:        market.TickSize.String(),
		MinSize:         market.MinSize,
		MaxSize:         market.MaxSize,
	}
	if market.Outcome != nil {
		s := market.Outcome.String()
		out.Outcome = &s
	}
	return out
}

func (handler *Handler) getMarket(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "id is required")
		return
	}
	market, err := handler.repo.GetMarket(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httputil.ErrorResponse(w, http.StatusNotFound, "market not found")
			return
		}
		handler.internalError(w, "loading market", err)
		return
	}
	_ = httputil.EncodeJSON(w, http.StatusOK, toMarketResponse(market))
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
	if err := handler.publisher.PublishMarketConfig(updated); err != nil {
		handler.publishFailed(w, "market-config after metadata update",
			"market metadata updated but config publish failed; please retry", id, err)
		return
	}
	_ = httputil.EncodeJSON(w, http.StatusOK, toMarketResponse(updated))
}

func (handler *Handler) pauseMarket(w http.ResponseWriter, r *http.Request) {
	handler.transitionMarket(w, r, StatusPaused)
}

// bulkPauseRequest is the POST /admin/markets/bulk-pause body.
type bulkPauseRequest struct {
	MarketIDs []string `json:"market_ids"`
}

// bulkMarketsResponse is the response shape for bulk market operations.
type bulkMarketsResponse struct {
	Markets []marketResponse `json:"markets"`
}

// bulkPauseMarkets pauses every market in the request body atomically.
// If any market is missing or not in Active status, the entire batch is
// rejected and no state changes — see Repository.PauseMarkets.
func (handler *Handler) bulkPauseMarkets(w http.ResponseWriter, r *http.Request) {
	var req bulkPauseRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.MarketIDs) == 0 {
		httputil.ErrorResponse(w, http.StatusBadRequest, "market_ids is required")
		return
	}

	updated, err := handler.repo.PauseMarkets(r.Context(), req.MarketIDs)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidMarket):
			httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrNotFound):
			httputil.ErrorResponse(w, http.StatusNotFound, err.Error())
		case errors.Is(err, ErrInvalidTransition):
			httputil.ErrorResponse(w, http.StatusConflict, err.Error())
		default:
			handler.internalError(w, "bulk-pausing markets", err)
		}
		return
	}

	statusField := zap.String("target_status", StatusPaused.String())
	for _, market := range updated {
		if err := handler.publisher.PublishMarketConfig(market); err != nil {
			handler.publishFailed(w, "market-config after bulk pause",
				"bulk pause persisted but config publish failed; please retry",
				market.ID, err, statusField)
			return
		}
		if err := handler.publisher.PublishStatusChange(r.Context(), market.ID, StatusPaused); err != nil {
			handler.publishFailed(w, "status change after bulk pause",
				"bulk pause persisted and config published, but status-change broadcast failed; please retry",
				market.ID, err, statusField)
			return
		}
	}

	resp := bulkMarketsResponse{}
	for _, market := range updated {
		resp.Markets = append(resp.Markets, toMarketResponse(market))
	}
	_ = httputil.EncodeJSON(w, http.StatusOK, resp)
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

	statusField := zap.String("target_status", target.String())
	if err := handler.publisher.PublishMarketConfig(updated); err != nil {
		handler.publishFailed(w, "market-config after status change",
			"market status updated but config publish failed; please retry", id, err, statusField)
		return
	}
	if err := handler.publisher.PublishStatusChange(r.Context(), id, target); err != nil {
		handler.publishFailed(w, "status change",
			"market status updated and config published, but status-change broadcast failed; please retry",
			id, err, statusField)
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

func toFeeRateResponse(market *Market) feeRateResponse {
	var bps int
	if market.FeeRateBps != nil {
		bps = int(*market.FeeRateBps)
	}
	return feeRateResponse{
		MarketID:   market.ID,
		FeeRateBps: bps,
		UpdatedAt:  market.UpdatedAt.UTC().Format(time.RFC3339),
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

	updated, err := handler.repo.UpdateFeeRate(r.Context(), marketID, req.FeeRateBps)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidFeeRate):
			httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrNotFound):
			httputil.ErrorResponse(w, http.StatusNotFound, "market not found")
		default:
			handler.internalError(w, "updating fee rate", err)
		}
		return
	}
	if err := handler.publisher.PublishMarketConfig(updated); err != nil {
		handler.publishFailed(w, "market-config after fee rate update",
			"fee rate updated but config publish failed; please retry", marketID, err)
		return
	}
	_ = httputil.EncodeJSON(w, http.StatusOK, toFeeRateResponse(updated))
}

// tradingConfigRequest is the PUT /admin/markets/{id}/trading-config body.
type tradingConfigRequest struct {
	TickSize string `json:"tick_size"`
	MinSize  int64  `json:"min_size"`
	MaxSize  *int64 `json:"max_size,omitempty"`
}

func (handler *Handler) setTradingConfig(w http.ResponseWriter, r *http.Request) {
	marketID := r.PathValue("id")
	if marketID == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "id is required")
		return
	}
	var req tradingConfigRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	tickSize, ok := ParseTickSize(req.TickSize)
	if !ok {
		httputil.ErrorResponse(w, http.StatusBadRequest, fmt.Sprintf("invalid tick_size %q", req.TickSize))
		return
	}
	if err := ValidateTradingConfigUpdate(tickSize, req.MinSize, req.MaxSize); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	updated, err := handler.repo.UpdateTradingConfig(r.Context(), marketID, tickSize, req.MinSize, req.MaxSize)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidMarket):
			httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrNotFound):
			httputil.ErrorResponse(w, http.StatusNotFound, "market not found")
		default:
			handler.internalError(w, "updating trading config", err)
		}
		return
	}
	if err := handler.publisher.PublishMarketConfig(updated); err != nil {
		handler.publishFailed(w, "market-config after trading-config update",
			"trading config updated but config publish failed; please retry", marketID, err)
		return
	}
	_ = httputil.EncodeJSON(w, http.StatusOK, toMarketResponse(updated))
}
