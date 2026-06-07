package market

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/http"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"

	"github.com/Shisa-Fosho/services/internal/shared/eth"
	"github.com/Shisa-Fosho/services/internal/shared/httputil"
)

// resolveEventRequest is the body for both resolve endpoints.
type resolveEventRequest struct {
	TxHash   string            `json:"tx_hash,omitempty"`
	Outcomes map[string]string `json:"outcomes"`
}

func (handler *Handler) resolveBinaryEvent(w http.ResponseWriter, r *http.Request) {
	eventID, req, ok := decodeResolveRequest(w, r)
	if !ok {
		return
	}

	_, siblings, ok := handler.loadEventForResolve(r.Context(), w, eventID, EventTypeBinary)
	if !ok {
		return
	}

	parsedOutcomes, ok := parseOutcomes(w, req.Outcomes)
	if !ok {
		return
	}

	siblingByID := indexByID(siblings)
	for marketID, declared := range parsedOutcomes {
		market, ok := siblingByID[marketID]
		if !ok {
			httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("market %s does not belong to event %s", marketID, eventID))
			return
		}
		cid := common.HexToHash(market.ConditionID)
		denom, err := handler.ct.PayoutDenominator(r.Context(), cid)
		if err != nil {
			handler.chainError(w, fmt.Sprintf("reading payoutDenominator for %s", marketID), err)
			return
		}
		if denom == nil || denom.Sign() == 0 {
			httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("market %s: %s", marketID, eth.ErrPayoutsNotReported.Error()))
			return
		}
		nums, err := handler.ct.PayoutNumerators(r.Context(), cid, 2)
		if err != nil {
			handler.chainError(w, fmt.Sprintf("reading payoutNumerators for %s", marketID), err)
			return
		}
		if len(nums) != 2 || nums[0] == nil || nums[1] == nil {
			httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("market %s: unexpected on-chain numerator shape", marketID))
			return
		}
		if !matchesDeclaredOutcome(nums[0], nums[1], declared) {
			httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("market %s: declared %s but on-chain numerators are [%s,%s]: %s",
					marketID, declared.String(), nums[0].String(), nums[1].String(),
					ErrOnChainMismatch.Error()))
			return
		}
	}

	handler.commitAndPublishResolve(r.Context(), w, eventID, parsedOutcomes)
}

func (handler *Handler) resolveNegRiskEvent(w http.ResponseWriter, r *http.Request) {
	eventID, req, ok := decodeResolveRequest(w, r)
	if !ok {
		return
	}

	event, siblings, ok := handler.loadEventForResolve(r.Context(), w, eventID, EventTypeNegRisk)
	if !ok {
		return
	}

	parsedOutcomes, ok := parseOutcomes(w, req.Outcomes)
	if !ok {
		return
	}

	siblingByID := indexByID(siblings)
	for marketID, declared := range parsedOutcomes {
		market, ok := siblingByID[marketID]
		if !ok {
			httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("market %s does not belong to event %s", marketID, eventID))
			return
		}
		cid := common.HexToHash(market.ConditionID)
		denom, err := handler.ct.PayoutDenominator(r.Context(), cid)
		if err != nil {
			handler.chainError(w, fmt.Sprintf("reading payoutDenominator for %s", marketID), err)
			return
		}
		if denom == nil || denom.Sign() == 0 {
			// Friendlier error: if the parent NegRisk market is already
			// determined, the admin is trying to declare a second YES on
			// a question that can never have its payouts reported.
			if event.NegRiskMarketID != nil {
				negID := common.HexToHash(*event.NegRiskMarketID)
				determined, dErr := handler.negRisk.MarketDetermined(r.Context(), negID)
				if dErr == nil && determined {
					httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
						"another market in this NegRisk event already resolved YES; this question can never be reported")
					return
				}
			}
			httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("market %s: %s", marketID, eth.ErrPayoutsNotReported.Error()))
			return
		}
		nums, err := handler.ct.PayoutNumerators(r.Context(), cid, 2)
		if err != nil {
			handler.chainError(w, fmt.Sprintf("reading payoutNumerators for %s", marketID), err)
			return
		}
		if len(nums) != 2 || nums[0] == nil || nums[1] == nil {
			httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("market %s: unexpected on-chain numerator shape", marketID))
			return
		}
		if !matchesDeclaredOutcome(nums[0], nums[1], declared) {
			httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("market %s: declared %s but on-chain numerators are [%s,%s]: %s",
					marketID, declared.String(), nums[0].String(), nums[1].String(),
					ErrOnChainMismatch.Error()))
			return
		}
	}

	handler.commitAndPublishResolve(r.Context(), w, eventID, parsedOutcomes)
}

func decodeResolveRequest(w http.ResponseWriter, r *http.Request) (string, resolveEventRequest, bool) {
	eventID := r.PathValue("id")
	if eventID == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "id is required")
		return "", resolveEventRequest{}, false
	}
	var req resolveEventRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return "", resolveEventRequest{}, false
	}
	if len(req.Outcomes) == 0 {
		httputil.ErrorResponse(w, http.StatusBadRequest, "outcomes is required")
		return "", resolveEventRequest{}, false
	}
	return eventID, req, true
}

// loadEventForResolve loads the event, asserts its EventType matches the
// expected type for this endpoint (422 if not), and loads its siblings.
// Returns false after writing an error response on any failure.
func (handler *Handler) loadEventForResolve(ctx context.Context, w http.ResponseWriter, eventID string, expected EventType) (*Event, []*Market, bool) {
	event, err := handler.repo.GetEvent(ctx, eventID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			httputil.ErrorResponse(w, http.StatusNotFound, "event not found")
			return nil, nil, false
		}
		handler.internalError(w, "loading event", err)
		return nil, nil, false
	}
	if event.EventType != expected {
		httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("event %s is %s, not %s", eventID, event.EventType.String(), expected.String()))
		return nil, nil, false
	}
	siblings, err := handler.repo.ListMarketsByEvent(ctx, eventID)
	if err != nil {
		handler.internalError(w, "listing markets", err)
		return nil, nil, false
	}
	return event, siblings, true
}

func parseOutcomes(w http.ResponseWriter, raw map[string]string) (map[string]Outcome, bool) {
	parsed := make(map[string]Outcome, len(raw))
	for marketID, outcomeStr := range raw {
		outcome, ok := ParseOutcome(outcomeStr)
		if !ok {
			httputil.ErrorResponse(w, http.StatusBadRequest,
				fmt.Sprintf("outcomes[%s] = %q is invalid (want YES or NO)", marketID, outcomeStr))
			return nil, false
		}
		parsed[marketID] = outcome
	}
	return parsed, true
}

func indexByID(markets []*Market) map[string]*Market {
	out := make(map[string]*Market, len(markets))
	for _, market := range markets {
		out[market.ID] = market
	}
	return out
}

// commitAndPublishResolve runs the DB transition and publishes KV + Core
// NATS for each market that actually resolved. Writes the success response
// or the appropriate error response.
func (handler *Handler) commitAndPublishResolve(ctx context.Context, w http.ResponseWriter, eventID string, parsedOutcomes map[string]Outcome) {
	updatedEvent, updatedMarkets, err := handler.repo.ResolveMarketsInEvent(ctx, eventID, parsedOutcomes)
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
			handler.internalError(w, "resolving markets", err)
		}
		return
	}

	for _, market := range updatedMarkets {
		outcome, transitioned := parsedOutcomes[market.ID]
		if !transitioned {
			continue
		}
		if err := handler.publisher.PublishMarketConfig(market); err != nil {
			handler.publishFailed(w, "market-config after resolve",
				"resolution persisted but config publish failed; please retry", market.ID, err)
			return
		}
		if err := handler.publisher.PublishStatusChangeWithOutcome(ctx, market.ID, StatusResolved, &outcome); err != nil {
			handler.publishFailed(w, "status-change after resolve",
				"resolution persisted and config published, but status-change broadcast failed; please retry",
				market.ID, err, zap.String("outcome", outcome.String()))
			return
		}
	}

	resp := eventWithMarketsResponse{Event: toEventResponse(updatedEvent)}
	for _, market := range updatedMarkets {
		resp.Markets = append(resp.Markets, toMarketResponse(market))
	}
	_ = httputil.EncodeJSON(w, http.StatusOK, resp)
}

// matchesDeclaredOutcome returns true when the on-chain numerators
// (index 0 = YES, index 1 = NO) match the admin's declared outcome.
func matchesDeclaredOutcome(yes, no *big.Int, declared Outcome) bool {
	switch declared {
	case OutcomeYes:
		return yes.Sign() > 0 && no.Sign() == 0
	case OutcomeNo:
		return yes.Sign() == 0 && no.Sign() > 0
	}
	return false
}
