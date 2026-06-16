package market

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Shisa-Fosho/services/internal/shared/eth"
	"github.com/Shisa-Fosho/services/internal/shared/httputil"
)

type addBinaryMarketsRequest struct {
	Market  createBinaryMarketSubobject   `json:"market"`
	Markets []createBinaryMarketSubobject `json:"markets,omitempty"`
}

type addNegRiskMarketsRequest struct {
	Market  createNegRiskMarketSubobject   `json:"market"`
	Markets []createNegRiskMarketSubobject `json:"markets,omitempty"`
}

func (handler *Handler) addBinaryMarkets(w http.ResponseWriter, r *http.Request) {
	event, ok := handler.loadEventForMarketAppend(r.Context(), w, r.PathValue("id"), EventTypeBinary)
	if !ok {
		return
	}
	var req addBinaryMarketsRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	markets, ok := handler.binaryMarketsFromAppendRequest(w, req)
	if !ok {
		return
	}
	if !handler.verifyOutcomeSlotCounts(r.Context(), w, markets) {
		return
	}
	if !handler.verifyBinaryTokenIDs(r.Context(), w, markets) {
		return
	}
	handler.finishAddMarkets(r.Context(), w, event.ID, markets)
}

func (handler *Handler) addNegRiskMarkets(w http.ResponseWriter, r *http.Request) {
	event, ok := handler.loadEventForMarketAppend(r.Context(), w, r.PathValue("id"), EventTypeNegRisk)
	if !ok {
		return
	}
	var req addNegRiskMarketsRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	markets, ok := handler.negRiskMarketsFromAppendRequest(w, req, *event.NegRiskMarketID)
	if !ok {
		return
	}
	for idx, market := range markets {
		qid := common.HexToHash(market.QuestionID)
		cid, err := handler.negRisk.ConditionID(r.Context(), qid)
		if err != nil {
			if errors.Is(err, eth.ErrNegRiskDisabled) {
				httputil.ErrorResponse(w, http.StatusBadGateway,
					"neg-risk adapter not configured on this deploy")
				return
			}
			handler.chainError(w, fmt.Sprintf("deriving neg-risk condition_id for markets[%d]", idx), err)
			return
		}
		market.ConditionID = cid.Hex()
	}
	if !handler.verifyOutcomeSlotCounts(r.Context(), w, markets) {
		return
	}
	if !handler.verifyNegRiskTokenIDs(r.Context(), w, markets) {
		return
	}
	handler.finishAddMarkets(r.Context(), w, event.ID, markets)
}

func (handler *Handler) binaryMarketsFromAppendRequest(w http.ResponseWriter, req addBinaryMarketsRequest) ([]*Market, bool) {
	if len(req.Markets) > 0 {
		httputil.ErrorResponse(w, http.StatusBadRequest, "binary append accepts exactly one market payload named market")
		return nil, false
	}
	return handler.binaryMarketsFromSubobjects(w, []createBinaryMarketSubobject{req.Market})
}

func (handler *Handler) negRiskMarketsFromAppendRequest(w http.ResponseWriter, req addNegRiskMarketsRequest, negRiskMarketID string) ([]*Market, bool) {
	if len(req.Markets) > 0 {
		httputil.ErrorResponse(w, http.StatusBadRequest, "neg-risk append accepts exactly one market payload named market")
		return nil, false
	}
	return handler.negRiskMarketsFromSubobjects(w, []createNegRiskMarketSubobject{req.Market}, negRiskMarketID)
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

func (handler *Handler) binaryMarketsFromSubobjects(w http.ResponseWriter, reqMarkets []createBinaryMarketSubobject) ([]*Market, bool) {
	if len(reqMarkets) == 0 {
		httputil.ErrorResponse(w, http.StatusBadRequest, "markets is required")
		return nil, false
	}
	markets := make([]*Market, 0, len(reqMarkets))
	for idx, marketReq := range reqMarkets {
		if marketReq.ConditionID == "" {
			httputil.ErrorResponse(w, http.StatusBadRequest,
				fmt.Sprintf("markets[%d].condition_id is required", idx))
			return nil, false
		}
		if !isHexHash(marketReq.ConditionID) {
			httputil.ErrorResponse(w, http.StatusBadRequest,
				fmt.Sprintf("markets[%d].condition_id must be a 0x-prefixed 32-byte hex string", idx))
			return nil, false
		}
		if marketReq.QuestionID == "" {
			httputil.ErrorResponse(w, http.StatusBadRequest,
				fmt.Sprintf("markets[%d].question_id is required", idx))
			return nil, false
		}
		if !isHexHash(marketReq.QuestionID) {
			httputil.ErrorResponse(w, http.StatusBadRequest,
				fmt.Sprintf("markets[%d].question_id must be a 0x-prefixed 32-byte hex string", idx))
			return nil, false
		}
		market, ok := marketFromBinarySubobject(w, idx, marketReq)
		if !ok {
			return nil, false
		}
		markets = append(markets, market)
	}
	return markets, true
}

func (handler *Handler) negRiskMarketsFromSubobjects(w http.ResponseWriter, reqMarkets []createNegRiskMarketSubobject, negRiskMarketID string) ([]*Market, bool) {
	if len(reqMarkets) == 0 {
		httputil.ErrorResponse(w, http.StatusBadRequest, "markets is required")
		return nil, false
	}
	adapterMarketID := common.HexToHash(negRiskMarketID)
	markets := make([]*Market, 0, len(reqMarkets))
	for idx, marketReq := range reqMarkets {
		if marketReq.QuestionID == "" {
			httputil.ErrorResponse(w, http.StatusBadRequest,
				fmt.Sprintf("markets[%d].question_id is required", idx))
			return nil, false
		}
		if !isHexHash(marketReq.QuestionID) {
			httputil.ErrorResponse(w, http.StatusBadRequest,
				fmt.Sprintf("markets[%d].question_id must be a 0x-prefixed 32-byte hex string", idx))
			return nil, false
		}
		if negRiskMarketIDOf(common.HexToHash(marketReq.QuestionID)) != adapterMarketID {
			httputil.ErrorResponse(w, http.StatusBadRequest,
				fmt.Sprintf("markets[%d].question_id does not belong to neg_risk_market_id (first 31 bytes must match)", idx))
			return nil, false
		}
		market, ok := marketFromNegRiskSubobject(w, idx, marketReq)
		if !ok {
			return nil, false
		}
		markets = append(markets, market)
	}
	return markets, true
}

func marketFromBinarySubobject(w http.ResponseWriter, idx int, marketReq createBinaryMarketSubobject) (*Market, bool) {
	if !validTokenIDs(w, idx, marketReq.TokenIDYes, marketReq.TokenIDNo) {
		return nil, false
	}
	tickSize, ok := ParseTickSize(marketReq.TickSize)
	if !ok {
		httputil.ErrorResponse(w, http.StatusBadRequest,
			fmt.Sprintf("markets[%d].tick_size %q is invalid", idx, marketReq.TickSize))
		return nil, false
	}
	return &Market{
		Slug:            marketReq.Slug,
		Question:        marketReq.Question,
		OutcomeYesLabel: marketReq.OutcomeYesLabel,
		OutcomeNoLabel:  marketReq.OutcomeNoLabel,
		TokenIDYes:      marketReq.TokenIDYes,
		TokenIDNo:       marketReq.TokenIDNo,
		ConditionID:     marketReq.ConditionID,
		QuestionID:      marketReq.QuestionID,
		Status:          StatusActive,
		TickSize:        tickSize,
		MinSize:         marketReq.MinSize,
		MaxSize:         marketReq.MaxSize,
		FeeRateBps:      marketReq.FeeRateBps,
	}, true
}

func marketFromNegRiskSubobject(w http.ResponseWriter, idx int, marketReq createNegRiskMarketSubobject) (*Market, bool) {
	if !validTokenIDs(w, idx, marketReq.TokenIDYes, marketReq.TokenIDNo) {
		return nil, false
	}
	tickSize, ok := ParseTickSize(marketReq.TickSize)
	if !ok {
		httputil.ErrorResponse(w, http.StatusBadRequest,
			fmt.Sprintf("markets[%d].tick_size %q is invalid", idx, marketReq.TickSize))
		return nil, false
	}
	return &Market{
		Slug:            marketReq.Slug,
		Question:        marketReq.Question,
		OutcomeYesLabel: marketReq.OutcomeYesLabel,
		OutcomeNoLabel:  marketReq.OutcomeNoLabel,
		TokenIDYes:      marketReq.TokenIDYes,
		TokenIDNo:       marketReq.TokenIDNo,
		QuestionID:      marketReq.QuestionID,
		Status:          StatusActive,
		TickSize:        tickSize,
		MinSize:         marketReq.MinSize,
		MaxSize:         marketReq.MaxSize,
		FeeRateBps:      marketReq.FeeRateBps,
	}, true
}

func (handler *Handler) finishAddMarkets(ctx context.Context, w http.ResponseWriter, eventID string, markets []*Market) {
	updatedEvent, addedMarkets, err := handler.repo.AddMarketsToEvent(ctx, eventID, markets)
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
			handler.internalError(w, "adding markets to event", err)
		}
		return
	}
	for _, market := range addedMarkets {
		if err := handler.publisher.PublishMarketConfig(market); err != nil {
			handler.publishFailed(w, "market-config after market append",
				"markets added but config publish failed; please retry", market.ID, err)
			return
		}
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
