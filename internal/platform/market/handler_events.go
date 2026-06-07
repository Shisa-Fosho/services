package market

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Shisa-Fosho/services/internal/shared/eth"
	"github.com/Shisa-Fosho/services/internal/shared/httputil"
)

// eventUpdateRequest is the PUT /admin/events/{id} body.
type eventUpdateRequest struct {
	Title             *string `json:"title,omitempty"`
	Description       *string `json:"description,omitempty"`
	CategoryID        *string `json:"category_id,omitempty"`
	Featured          *bool   `json:"featured,omitempty"`
	FeaturedSortOrder *int16  `json:"featured_sort_order,omitempty"`
}

// eventResponse is the JSON shape returned for a single event.
type eventResponse struct {
	ID                string  `json:"id"`
	Slug              string  `json:"slug"`
	Title             string  `json:"title"`
	Description       string  `json:"description"`
	CategoryID        string  `json:"category_id"`
	EventType         string  `json:"event_type"`
	Status            string  `json:"status"`
	EndDate           string  `json:"end_date"`
	Featured          bool    `json:"featured"`
	FeaturedSortOrder int16   `json:"featured_sort_order"`
	NegRiskMarketID   *string `json:"neg_risk_market_id,omitempty"`
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
		NegRiskMarketID:   event.NegRiskMarketID,
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

// voidEventRequest is the POST /admin/events/{id}/void body. market_ids may
// be omitted to default to "every market currently in Active status".
// NegRisk events return 400 unconditionally — voiding NegRisk markets is
// deferred (no on-chain primitive).
type voidEventRequest struct {
	TxHash    string   `json:"tx_hash,omitempty"`
	MarketIDs []string `json:"market_ids,omitempty"`
}

func (handler *Handler) voidEvent(w http.ResponseWriter, r *http.Request) {
	eventID := r.PathValue("id")
	if eventID == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "id is required")
		return
	}

	if _, ok := handler.loadEventForVoid(r.Context(), w, eventID); !ok {
		return
	}

	marketIDs, siblingByID, ok := handler.resolveVoidTargets(w, r, eventID)
	if !ok {
		return
	}

	if !handler.verifyVoidChainState(r.Context(), w, siblingByID, marketIDs) {
		return
	}

	updatedEvent, updatedMarkets, err := handler.repo.VoidMarketsInEvent(r.Context(), eventID, marketIDs)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			httputil.ErrorResponse(w, http.StatusNotFound, "event not found")
		case errors.Is(err, ErrInvalidMarket):
			httputil.ErrorResponse(w, http.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, ErrInvalidTransition):
			httputil.ErrorResponse(w, http.StatusConflict, err.Error())
		case errors.Is(err, ErrInvalidEvent):
			httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		default:
			handler.internalError(w, "voiding markets", err)
		}
		return
	}

	if !handler.publishVoidedMarkets(r.Context(), w, updatedMarkets, marketIDs) {
		return
	}

	resp := eventWithMarketsResponse{Event: toEventResponse(updatedEvent)}
	for _, market := range updatedMarkets {
		resp.Markets = append(resp.Markets, toMarketResponse(market))
	}
	_ = httputil.EncodeJSON(w, http.StatusOK, resp)
}

// loadEventForVoid loads the event by ID and rejects NegRisk events with
// 400 (voiding NegRisk is not yet supported). Returns false after writing
// the appropriate error response on any failure.
func (handler *Handler) loadEventForVoid(ctx context.Context, w http.ResponseWriter, eventID string) (*Event, bool) {
	event, err := handler.repo.GetEvent(ctx, eventID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httputil.ErrorResponse(w, http.StatusNotFound, "event not found")
			return nil, false
		}
		handler.internalError(w, "loading event", err)
		return nil, false
	}
	if event.EventType == EventTypeNegRisk {
		httputil.ErrorResponse(w, http.StatusBadRequest,
			"void_not_supported_for_negrisk: voiding NegRisk markets is not yet supported")
		return nil, false
	}
	return event, true
}

// resolveVoidTargets decodes the optional request body, lists the event's
// siblings, applies the default-all-active fallback when no market_ids are
// supplied, and validates that every target belongs to the event. Returns
// the resolved market_id list and a sibling lookup map.
func (handler *Handler) resolveVoidTargets(w http.ResponseWriter, r *http.Request, eventID string) ([]string, map[string]*Market, bool) {
	var req voidEventRequest
	if r.ContentLength > 0 {
		if err := httputil.DecodeJSON(r, &req); err != nil {
			httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
			return nil, nil, false
		}
	}

	siblings, err := handler.repo.ListMarketsByEvent(r.Context(), eventID)
	if err != nil {
		handler.internalError(w, "listing markets", err)
		return nil, nil, false
	}
	siblingByID := make(map[string]*Market, len(siblings))
	for _, market := range siblings {
		siblingByID[market.ID] = market
	}

	marketIDs := req.MarketIDs
	if len(marketIDs) == 0 {
		for _, market := range siblings {
			if market.Status == StatusActive {
				marketIDs = append(marketIDs, market.ID)
			}
		}
	}
	if len(marketIDs) == 0 {
		httputil.ErrorResponse(w, http.StatusUnprocessableEntity, "no markets to void")
		return nil, nil, false
	}

	for _, marketID := range marketIDs {
		if _, ok := siblingByID[marketID]; !ok {
			httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("market %s does not belong to event %s", marketID, eventID))
			return nil, nil, false
		}
	}
	return marketIDs, siblingByID, true
}

// verifyVoidChainState confirms each market has reportPayouts called with
// the binary void pattern (equal positive numerators). Returns false after
// writing an error response on any failure.
func (handler *Handler) verifyVoidChainState(ctx context.Context, w http.ResponseWriter, siblingByID map[string]*Market, marketIDs []string) bool {
	for _, marketID := range marketIDs {
		market := siblingByID[marketID]
		cid := common.HexToHash(market.ConditionID)
		denom, err := handler.ct.PayoutDenominator(ctx, cid)
		if err != nil {
			handler.chainError(w, fmt.Sprintf("reading payoutDenominator for %s", marketID), err)
			return false
		}
		if denom == nil || denom.Sign() == 0 {
			httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("market %s: %s", marketID, eth.ErrPayoutsNotReported.Error()))
			return false
		}
		nums, err := handler.ct.PayoutNumerators(ctx, cid, 2)
		if err != nil {
			handler.chainError(w, fmt.Sprintf("reading payoutNumerators for %s", marketID), err)
			return false
		}
		if len(nums) != 2 || nums[0] == nil || nums[1] == nil {
			httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("market %s: unexpected on-chain numerator shape", marketID))
			return false
		}
		if nums[0].Sign() == 0 || nums[1].Sign() == 0 || nums[0].Cmp(nums[1]) != 0 {
			httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("market %s: void pattern requires equal positive numerators, got [%s,%s]: %s",
					marketID, nums[0].String(), nums[1].String(), ErrOnChainMismatch.Error()))
			return false
		}
	}
	return true
}

// publishVoidedMarkets publishes KV + Core NATS for every voided market.
// Returns false after writing a 502 response on the first publish failure.
func (handler *Handler) publishVoidedMarkets(ctx context.Context, w http.ResponseWriter, updatedMarkets []*Market, marketIDs []string) bool {
	voidedSet := make(map[string]struct{}, len(marketIDs))
	for _, marketID := range marketIDs {
		voidedSet[marketID] = struct{}{}
	}
	for _, market := range updatedMarkets {
		if _, ok := voidedSet[market.ID]; !ok {
			continue
		}
		if err := handler.publisher.PublishMarketConfig(market); err != nil {
			handler.publishFailed(w, "market-config after void",
				"void persisted but config publish failed; please retry", market.ID, err)
			return false
		}
		if err := handler.publisher.PublishStatusChangeWithOutcome(ctx, market.ID, StatusVoided, nil); err != nil {
			handler.publishFailed(w, "status-change after void",
				"void persisted and config published, but status-change broadcast failed; please retry",
				market.ID, err)
			return false
		}
	}
	return true
}

// eventWithMarketsResponse is the response shape for endpoints that return
// an event alongside its child markets (create, resolve, void).
type eventWithMarketsResponse struct {
	Event   eventResponse    `json:"event"`
	Markets []marketResponse `json:"markets"`
}
