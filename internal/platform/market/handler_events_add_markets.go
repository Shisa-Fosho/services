package market

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/Shisa-Fosho/services/internal/shared/httputil"
)

type addBinaryMarketRequest struct {
	Market createBinaryMarketSubobject `json:"market"`
}

type addNegRiskMarketRequest struct {
	Market createNegRiskMarketSubobject `json:"market"`
}

func (handler *Handler) addBinaryMarket(w http.ResponseWriter, r *http.Request) {
	event, ok := handler.loadEventForMarketAppend(r.Context(), w, r.PathValue("id"), EventTypeBinary)
	if !ok {
		return
	}
	var req addBinaryMarketRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	market, ok := binaryMarketFromSubobject(w, req.Market)
	if !ok {
		return
	}
	if !handler.verifyOutcomeSlotCount(r.Context(), w, market) {
		return
	}
	if !handler.verifyBinaryTokenIDs(r.Context(), w, market) {
		return
	}
	handler.finishAddMarket(r.Context(), w, event.ID, market)
}

func (handler *Handler) addNegRiskMarket(w http.ResponseWriter, r *http.Request) {
	event, ok := handler.loadEventForMarketAppend(r.Context(), w, r.PathValue("id"), EventTypeNegRisk)
	if !ok {
		return
	}
	var req addNegRiskMarketRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	market, ok := negRiskMarketFromSubobject(w, req.Market, *event.NegRiskMarketID)
	if !ok {
		return
	}
	if !handler.deriveNegRiskConditionID(r.Context(), w, market) {
		return
	}
	if !handler.verifyOutcomeSlotCount(r.Context(), w, market) {
		return
	}
	if !handler.verifyNegRiskTokenIDs(r.Context(), w, market) {
		return
	}
	handler.finishAddMarket(r.Context(), w, event.ID, market)
}

func (handler *Handler) loadEventForMarketAppend(ctx context.Context, w http.ResponseWriter, eventID string, expected EventType) (*Event, bool) {
	if eventID == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "id is required")
		return nil, false
	}
	event, err := handler.repo.GetEvent(ctx, eventID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httputil.ErrorResponse(w, http.StatusNotFound, "event not found")
			return nil, false
		}
		handler.internalError(w, "loading event", err)
		return nil, false
	}
	if event.EventType != expected {
		httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("event %s is %s, not %s", eventID, event.EventType.String(), expected.String()))
		return nil, false
	}
	if event.Status.IsTerminal() {
		httputil.ErrorResponse(w, http.StatusConflict,
			fmt.Sprintf("event %s is terminal (%s)", eventID, event.Status.String()))
		return nil, false
	}
	return event, true
}

func (handler *Handler) finishAddMarket(ctx context.Context, w http.ResponseWriter, eventID string, market *Market) {
	updatedEvent, addedMarket, err := handler.repo.AddMarketToEvent(ctx, eventID, market)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			httputil.ErrorResponse(w, http.StatusNotFound, "event not found")
		case errors.Is(err, ErrDuplicateSlug):
			httputil.ErrorResponse(w, http.StatusConflict, "slug, condition_id, or question_id already in use")
		case errors.Is(err, ErrInvalidTransition):
			httputil.ErrorResponse(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrInvalidEvent):
			httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrInvalidMarket):
			httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		default:
			handler.internalError(w, "adding market to event", err)
		}
		return
	}
	if err := handler.publisher.PublishMarketConfig(addedMarket); err != nil {
		handler.publishFailed(w, "market-config after market append",
			"market added but config publish failed; please retry", addedMarket.ID, err)
		return
	}
	allMarkets, err := handler.repo.ListMarketsByEvent(ctx, eventID)
	if err != nil {
		handler.internalError(w, "listing markets after append", err)
		return
	}
	resp := eventWithMarketsResponse{Event: toEventResponse(updatedEvent)}
	for _, market := range allMarkets {
		resp.Markets = append(resp.Markets, toMarketResponse(market))
	}
	_ = httputil.EncodeJSON(w, http.StatusOK, resp)
}
