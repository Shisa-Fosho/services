package market

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/Shisa-Fosho/services/internal/shared/eth"
	"github.com/Shisa-Fosho/services/internal/shared/httputil"
)

// createBinaryEventRequest is the POST /admin/events/binary body. Creation
// accepts exactly one initial market because direct ConditionalTokens
// prepareCondition(...) creates one condition per on-chain transaction;
// additional markets are appended one at a time afterward.
type createBinaryEventRequest struct {
	Slug        string                `json:"slug"`
	Title       string                `json:"title"`
	Description string                `json:"description"`
	CategoryID  string                `json:"category_id"`
	EndDate     time.Time             `json:"end_date"`
	Market      binaryMarketSubobject `json:"market"`
}

type binaryMarketSubobject struct {
	Slug            string `json:"slug"`
	Question        string `json:"question"`
	OutcomeYesLabel string `json:"outcome_yes_label"`
	OutcomeNoLabel  string `json:"outcome_no_label"`
	TokenIDYes      string `json:"token_id_yes"`
	TokenIDNo       string `json:"token_id_no"`
	ConditionID     string `json:"condition_id"`
	QuestionID      string `json:"question_id"`
	TickSize        string `json:"tick_size"`
	MinSize         int64  `json:"min_size"`
	MaxSize         *int64 `json:"max_size,omitempty"`
	FeeRateBps      *int64 `json:"fee_rate_bps,omitempty"`
}

// createNegRiskEventRequest is the POST /admin/events/neg-risk body. The
// event-level neg_risk_market_id is required, and the single initial market
// supplies only question_id — condition_id is derived server-side from the
// NegRiskAdapter so the admin can't supply an inconsistent value.
type createNegRiskEventRequest struct {
	Slug            string                 `json:"slug"`
	Title           string                 `json:"title"`
	Description     string                 `json:"description"`
	CategoryID      string                 `json:"category_id"`
	EndDate         time.Time              `json:"end_date"`
	NegRiskMarketID string                 `json:"neg_risk_market_id"`
	Market          negRiskMarketSubobject `json:"market"`
}

type negRiskMarketSubobject struct {
	Slug            string `json:"slug"`
	Question        string `json:"question"`
	OutcomeYesLabel string `json:"outcome_yes_label"`
	OutcomeNoLabel  string `json:"outcome_no_label"`
	TokenIDYes      string `json:"token_id_yes"`
	TokenIDNo       string `json:"token_id_no"`
	QuestionID      string `json:"question_id"`
	TickSize        string `json:"tick_size"`
	MinSize         int64  `json:"min_size"`
	MaxSize         *int64 `json:"max_size,omitempty"`
	FeeRateBps      *int64 `json:"fee_rate_bps,omitempty"`
}

func (handler *Handler) createBinaryEvent(w http.ResponseWriter, r *http.Request) {
	var req createBinaryEventRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}

	market, ok := parseBinaryMarket(w, req.Market)
	if !ok {
		return
	}
	if !handler.verifyOutcomeSlotCount(r.Context(), w, market) {
		return
	}
	if !handler.verifyBinaryTokenIDs(r.Context(), w, market) {
		return
	}

	event := &Event{
		Slug:             req.Slug,
		Title:            req.Title,
		Description:      req.Description,
		CategoryID:       req.CategoryID,
		EventType:        EventTypeBinary,
		ResolutionConfig: json.RawMessage(`{}`),
		Status:           StatusPaused,
		EndDate:          req.EndDate,
	}

	handler.finishCreate(r.Context(), w, event, market)
}

func (handler *Handler) createNegRiskEvent(w http.ResponseWriter, r *http.Request) {
	var req createNegRiskEventRequest
	if err := httputil.DecodeJSON(r, &req); err != nil {
		httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.NegRiskMarketID == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "neg_risk_market_id is required")
		return
	}
	if !isHexHash(req.NegRiskMarketID) {
		httputil.ErrorResponse(w, http.StatusBadRequest,
			"neg_risk_market_id must be a 0x-prefixed 32-byte hex string")
		return
	}
	// Per NegRiskIdLib (neg-risk-ctf-adapter v2.0.0), MarketIds always have
	// their final byte zeroed; a non-zero byte means the admin pasted a
	// questionId where the marketId belongs.
	if common.HexToHash(req.NegRiskMarketID)[31] != 0 {
		httputil.ErrorResponse(w, http.StatusBadRequest,
			"neg_risk_market_id is not a NegRisk marketId (final byte must be zero)")
		return
	}

	market, ok := parseNegRiskMarket(w, req.Market, req.NegRiskMarketID)
	if !ok {
		return
	}

	// Derive condition_id via the NegRiskAdapter. Unlike CT (where conditionId
	// is a pure hash the admin can compute off-chain), the adapter applies its
	// own salts, so we own this derivation.
	if !handler.deriveNegRiskConditionID(r.Context(), w, market) {
		return
	}
	if !handler.verifyOutcomeSlotCount(r.Context(), w, market) {
		return
	}
	if !handler.verifyNegRiskTokenIDs(r.Context(), w, market) {
		return
	}

	negRiskID := req.NegRiskMarketID
	event := &Event{
		Slug:             req.Slug,
		Title:            req.Title,
		Description:      req.Description,
		CategoryID:       req.CategoryID,
		EventType:        EventTypeNegRisk,
		ResolutionConfig: json.RawMessage(`{}`),
		Status:           StatusPaused,
		EndDate:          req.EndDate,
		NegRiskMarketID:  &negRiskID,
	}

	handler.finishCreate(r.Context(), w, event, market)
}

// parseBinaryMarket validates one binary market payload (the create
// initial market or an append market) and builds the domain Market.
// condition_id and question_id must be present and well-formed hex; token ids
// and tick size are checked too. Writes a 400 and returns false on violation.
func parseBinaryMarket(w http.ResponseWriter, marketReq binaryMarketSubobject) (*Market, bool) {
	if marketReq.ConditionID == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "market.condition_id is required")
		return nil, false
	}
	if !isHexHash(marketReq.ConditionID) {
		httputil.ErrorResponse(w, http.StatusBadRequest,
			"market.condition_id must be a 0x-prefixed 32-byte hex string")
		return nil, false
	}
	if marketReq.QuestionID == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "market.question_id is required")
		return nil, false
	}
	if !isHexHash(marketReq.QuestionID) {
		httputil.ErrorResponse(w, http.StatusBadRequest,
			"market.question_id must be a 0x-prefixed 32-byte hex string")
		return nil, false
	}
	if !validTokenIDs(w, marketReq.TokenIDYes, marketReq.TokenIDNo) {
		return nil, false
	}
	tickSize, ok := ParseTickSize(marketReq.TickSize)
	if !ok {
		httputil.ErrorResponse(w, http.StatusBadRequest,
			fmt.Sprintf("market.tick_size %q is invalid", marketReq.TickSize))
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
		Status:          StatusPaused,
		TickSize:        tickSize,
		MinSize:         marketReq.MinSize,
		MaxSize:         marketReq.MaxSize,
		FeeRateBps:      marketReq.FeeRateBps,
	}, true
}

// parseNegRiskMarket validates one NegRisk market payload against the
// event's neg_risk_market_id and builds the domain Market. condition_id is
// left empty — the caller derives it from the adapter. Writes a 400 and
// returns false on violation.
func parseNegRiskMarket(w http.ResponseWriter, marketReq negRiskMarketSubobject, negRiskMarketID string) (*Market, bool) {
	if marketReq.QuestionID == "" {
		httputil.ErrorResponse(w, http.StatusBadRequest, "market.question_id is required")
		return nil, false
	}
	if !isHexHash(marketReq.QuestionID) {
		httputil.ErrorResponse(w, http.StatusBadRequest,
			"market.question_id must be a 0x-prefixed 32-byte hex string")
		return nil, false
	}
	// QuestionIds share their first 31 bytes with the parent MarketId; the
	// final byte is the question index (NegRiskIdLib). Reject a question that
	// belongs to a different adapter market — otherwise the stored grouping is
	// wrong and the resolve-time getDetermined check would query the wrong one.
	if negRiskMarketIDOf(common.HexToHash(marketReq.QuestionID)) != common.HexToHash(negRiskMarketID) {
		httputil.ErrorResponse(w, http.StatusBadRequest,
			"market.question_id does not belong to neg_risk_market_id (first 31 bytes must match)")
		return nil, false
	}
	if !validTokenIDs(w, marketReq.TokenIDYes, marketReq.TokenIDNo) {
		return nil, false
	}
	tickSize, ok := ParseTickSize(marketReq.TickSize)
	if !ok {
		httputil.ErrorResponse(w, http.StatusBadRequest,
			fmt.Sprintf("market.tick_size %q is invalid", marketReq.TickSize))
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
		Status:          StatusPaused,
		TickSize:        tickSize,
		MinSize:         marketReq.MinSize,
		MaxSize:         marketReq.MaxSize,
		FeeRateBps:      marketReq.FeeRateBps,
	}, true
}

// deriveNegRiskConditionID sets market.ConditionID from the NegRiskAdapter's
// getConditionId(questionId). Writes a 502 and returns false if the adapter
// isn't configured or the chain read fails.
func (handler *Handler) deriveNegRiskConditionID(ctx context.Context, w http.ResponseWriter, market *Market) bool {
	cid, err := handler.negRisk.ConditionID(ctx, common.HexToHash(market.QuestionID))
	if err != nil {
		if errors.Is(err, eth.ErrNegRiskDisabled) {
			httputil.ErrorResponse(w, http.StatusBadGateway,
				"neg-risk adapter not configured on this deploy")
			return false
		}
		handler.chainError(w, "deriving neg-risk condition_id", err)
		return false
	}
	market.ConditionID = cid.Hex()
	return true
}

// isHexHash reports whether value is a 0x-prefixed 32-byte hex string —
// the wire format for conditionIds, questionIds, and adapter marketIds.
// common.HexToHash silently zero-fills malformed input, so reject bad
// values at the boundary where an accurate error is still possible.
func isHexHash(value string) bool {
	if len(value) != 66 || value[0] != '0' || (value[1] != 'x' && value[1] != 'X') {
		return false
	}
	for _, char := range value[2:] {
		switch {
		case char >= '0' && char <= '9':
		case char >= 'a' && char <= 'f':
		case char >= 'A' && char <= 'F':
		default:
			return false
		}
	}
	return true
}

// negRiskMarketIDOf returns the adapter MarketId a questionId belongs to:
// the questionId with its final byte (the question index) zeroed, per
// NegRiskIdLib.getMarketId in neg-risk-ctf-adapter v2.0.0.
func negRiskMarketIDOf(questionID common.Hash) common.Hash {
	questionID[31] = 0
	return questionID
}

// validTokenIDs checks the wire format of a market's token id pair:
// non-empty decimal uint256 strings (the ERC1155 position ids as the
// Polymarket SDKs render them), and YES distinct from NO. Writes a 400
// and returns false on violation.
func validTokenIDs(w http.ResponseWriter, tokenIDYes, tokenIDNo string) bool {
	if parseTokenID(tokenIDYes) == nil {
		httputil.ErrorResponse(w, http.StatusBadRequest,
			"market.token_id_yes must be a decimal uint256 string")
		return false
	}
	if parseTokenID(tokenIDNo) == nil {
		httputil.ErrorResponse(w, http.StatusBadRequest,
			"market.token_id_no must be a decimal uint256 string")
		return false
	}
	if tokenIDYes == tokenIDNo {
		httputil.ErrorResponse(w, http.StatusBadRequest,
			"market: token_id_yes and token_id_no must differ")
		return false
	}
	return true
}

// parseTokenID parses a canonical decimal uint256 token id, returning
// nil when the string is empty, non-decimal, exceeds 256 bits, or is
// not the canonical rendering (leading zeros, "+" sign) — the stored
// string is matched against token ids elsewhere, so only one rendering
// per value may pass.
func parseTokenID(value string) *big.Int {
	parsed, ok := new(big.Int).SetString(value, 10)
	if !ok || parsed.Sign() < 0 || parsed.BitLen() > 256 || parsed.String() != value {
		return nil
	}
	return parsed
}

// verifyBinaryTokenIDs confirms the market's token id pair matches the
// on-chain derivation for its conditionId under the configured collateral
// token. A mismatch means the admin pasted ids that don't correspond to
// the condition — orders would trade tokens settlement can't redeem.
// Writes the response and returns false on failure.
func (handler *Handler) verifyBinaryTokenIDs(ctx context.Context, w http.ResponseWriter, market *Market) bool {
	yes, no, err := handler.ct.PositionIDs(ctx, handler.collateral, common.HexToHash(market.ConditionID))
	if err != nil {
		handler.chainError(w, "deriving position ids for market", err)
		return false
	}
	return handler.tokenIDsMatch(w, market, yes, no)
}

// verifyNegRiskTokenIDs is the NEG_RISK counterpart of verifyBinaryTokenIDs:
// the adapter derives position ids from the questionId against its wrapped
// collateral.
func (handler *Handler) verifyNegRiskTokenIDs(ctx context.Context, w http.ResponseWriter, market *Market) bool {
	yes, no, err := handler.negRisk.PositionIDs(ctx, common.HexToHash(market.QuestionID))
	if err != nil {
		handler.chainError(w, "deriving neg-risk position ids for market", err)
		return false
	}
	return handler.tokenIDsMatch(w, market, yes, no)
}

// tokenIDsMatch compares a market's stored token ids against the derived
// (YES, NO) pair, writing a 422 and returning false on mismatch.
func (handler *Handler) tokenIDsMatch(w http.ResponseWriter, market *Market, yes, no *big.Int) bool {
	storedYes := parseTokenID(market.TokenIDYes)
	if storedYes == nil || storedYes.Cmp(yes) != 0 {
		httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("market.token_id_yes does not match on-chain derivation %s: %s",
				yes.String(), ErrOnChainMismatch.Error()))
		return false
	}
	storedNo := parseTokenID(market.TokenIDNo)
	if storedNo == nil || storedNo.Cmp(no) != 0 {
		httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("market.token_id_no does not match on-chain derivation %s: %s",
				no.String(), ErrOnChainMismatch.Error()))
		return false
	}
	return true
}

// verifyOutcomeSlotCount confirms the market's conditionId has been prepared
// on ConditionalTokens with exactly two outcome slots. Writes the response
// and returns false on failure.
func (handler *Handler) verifyOutcomeSlotCount(ctx context.Context, w http.ResponseWriter, market *Market) bool {
	count, err := handler.ct.OutcomeSlotCount(ctx, common.HexToHash(market.ConditionID))
	if err != nil {
		handler.chainError(w, "reading getOutcomeSlotCount for market", err)
		return false
	}
	if count == 0 {
		httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("market: %s", eth.ErrConditionNotPrepared.Error()))
		return false
	}
	if count != 2 {
		httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("market: outcome slot count %d != 2", count))
		return false
	}
	return true
}

// finishCreate persists the event + market and publishes config to KV.
// No Core NATS publish on create — status didn't change from a client's
// perspective; clients refresh listings instead.
func (handler *Handler) finishCreate(ctx context.Context, w http.ResponseWriter, event *Event, market *Market) {
	createdEvent, createdMarket, err := handler.repo.CreateEventWithMarket(ctx, event, market)
	if err != nil {
		switch {
		case errors.Is(err, ErrDuplicateSlug):
			httputil.ErrorResponse(w, http.StatusConflict, "slug, condition_id, or question_id already in use")
		case errors.Is(err, ErrInvalidEvent):
			httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, ErrInvalidMarket):
			httputil.ErrorResponse(w, http.StatusBadRequest, err.Error())
		default:
			handler.internalError(w, "creating event", err)
		}
		return
	}

	if err := handler.publisher.PublishMarketConfig(createdMarket); err != nil {
		handler.publishFailed(w, "market-config after event creation",
			"event created but config publish failed; please retry", createdMarket.ID, err)
		return
	}

	resp := eventWithMarketsResponse{Event: toEventResponse(createdEvent)}
	resp.Markets = append(resp.Markets, toMarketResponse(createdMarket))
	_ = httputil.EncodeJSON(w, http.StatusCreated, resp)
}
