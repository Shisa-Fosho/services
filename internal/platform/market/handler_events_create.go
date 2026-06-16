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
// prepareCondition(...) creates one condition per on-chain transaction.
type createBinaryEventRequest struct {
	Slug        string                        `json:"slug"`
	Title       string                        `json:"title"`
	Description string                        `json:"description"`
	CategoryID  string                        `json:"category_id"`
	EndDate     time.Time                     `json:"end_date"`
	Market      createBinaryMarketSubobject   `json:"market"`
	Markets     []createBinaryMarketSubobject `json:"markets,omitempty"`
}

type createBinaryMarketSubobject struct {
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
	Slug            string                         `json:"slug"`
	Title           string                         `json:"title"`
	Description     string                         `json:"description"`
	CategoryID      string                         `json:"category_id"`
	EndDate         time.Time                      `json:"end_date"`
	NegRiskMarketID string                         `json:"neg_risk_market_id"`
	Market          createNegRiskMarketSubobject   `json:"market"`
	Markets         []createNegRiskMarketSubobject `json:"markets,omitempty"`
}

type createNegRiskMarketSubobject struct {
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

	markets, ok := handler.binaryMarketsFromCreateRequest(w, req)
	if !ok {
		return
	}
	if !handler.verifyOutcomeSlotCounts(r.Context(), w, markets) {
		return
	}
	if !handler.verifyBinaryTokenIDs(r.Context(), w, markets) {
		return
	}

	event := &Event{
		Slug:             req.Slug,
		Title:            req.Title,
		Description:      req.Description,
		CategoryID:       req.CategoryID,
		EventType:        EventTypeBinary,
		ResolutionConfig: json.RawMessage(`{}`),
		Status:           StatusActive,
		EndDate:          req.EndDate,
	}

	handler.finishCreate(r.Context(), w, event, markets)
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
	adapterMarketID := common.HexToHash(req.NegRiskMarketID)
	// Per NegRiskIdLib (neg-risk-ctf-adapter v2.0.0), MarketIds always have
	// their final byte zeroed; a non-zero byte means the admin pasted a
	// questionId where the marketId belongs.
	if adapterMarketID[31] != 0 {
		httputil.ErrorResponse(w, http.StatusBadRequest,
			"neg_risk_market_id is not a NegRisk marketId (final byte must be zero)")
		return
	}

	markets, ok := handler.negRiskMarketsFromCreateRequest(w, req)
	if !ok {
		return
	}

	// Derive condition_id per market via the NegRiskAdapter. Unlike CT
	// (where conditionId is a pure hash the admin can compute off-chain),
	// the adapter applies its own salts, so we own this derivation.
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

	negRiskID := req.NegRiskMarketID
	event := &Event{
		Slug:             req.Slug,
		Title:            req.Title,
		Description:      req.Description,
		CategoryID:       req.CategoryID,
		EventType:        EventTypeNegRisk,
		ResolutionConfig: json.RawMessage(`{}`),
		Status:           StatusActive,
		EndDate:          req.EndDate,
		NegRiskMarketID:  &negRiskID,
	}

	handler.finishCreate(r.Context(), w, event, markets)
}

func (handler *Handler) binaryMarketsFromCreateRequest(w http.ResponseWriter, req createBinaryEventRequest) ([]*Market, bool) {
	if len(req.Markets) > 0 {
		httputil.ErrorResponse(w, http.StatusBadRequest, "binary event creation accepts exactly one initial market payload named market")
		return nil, false
	}
	return handler.binaryMarketsFromSubobjects(w, []createBinaryMarketSubobject{req.Market})
}

func (handler *Handler) negRiskMarketsFromCreateRequest(w http.ResponseWriter, req createNegRiskEventRequest) ([]*Market, bool) {
	if len(req.Markets) > 0 {
		httputil.ErrorResponse(w, http.StatusBadRequest, "neg-risk event creation accepts exactly one initial market payload named market")
		return nil, false
	}
	return handler.negRiskMarketsFromSubobjects(w, []createNegRiskMarketSubobject{req.Market}, req.NegRiskMarketID)
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
func validTokenIDs(w http.ResponseWriter, idx int, tokenIDYes, tokenIDNo string) bool {
	if parseTokenID(tokenIDYes) == nil {
		httputil.ErrorResponse(w, http.StatusBadRequest,
			fmt.Sprintf("markets[%d].token_id_yes must be a decimal uint256 string", idx))
		return false
	}
	if parseTokenID(tokenIDNo) == nil {
		httputil.ErrorResponse(w, http.StatusBadRequest,
			fmt.Sprintf("markets[%d].token_id_no must be a decimal uint256 string", idx))
		return false
	}
	if tokenIDYes == tokenIDNo {
		httputil.ErrorResponse(w, http.StatusBadRequest,
			fmt.Sprintf("markets[%d]: token_id_yes and token_id_no must differ", idx))
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

// verifyBinaryTokenIDs confirms each market's token id pair matches the
// on-chain derivation for its conditionId under the configured collateral
// token. A mismatch means the admin pasted ids that don't correspond to
// the condition — orders would trade tokens settlement can't redeem.
// Writes the response and returns false on any failure.
func (handler *Handler) verifyBinaryTokenIDs(ctx context.Context, w http.ResponseWriter, markets []*Market) bool {
	for idx, market := range markets {
		yes, no, err := handler.ct.PositionIDs(ctx, handler.collateral, common.HexToHash(market.ConditionID))
		if err != nil {
			handler.chainError(w, fmt.Sprintf("deriving position ids for markets[%d]", idx), err)
			return false
		}
		if !handler.tokenIDsMatch(w, idx, market, yes, no) {
			return false
		}
	}
	return true
}

// verifyNegRiskTokenIDs is the NEG_RISK counterpart of
// verifyBinaryTokenIDs: the adapter derives position ids from the
// questionId against its wrapped collateral.
func (handler *Handler) verifyNegRiskTokenIDs(ctx context.Context, w http.ResponseWriter, markets []*Market) bool {
	for idx, market := range markets {
		yes, no, err := handler.negRisk.PositionIDs(ctx, common.HexToHash(market.QuestionID))
		if err != nil {
			handler.chainError(w, fmt.Sprintf("deriving neg-risk position ids for markets[%d]", idx), err)
			return false
		}
		if !handler.tokenIDsMatch(w, idx, market, yes, no) {
			return false
		}
	}
	return true
}

// tokenIDsMatch compares a market's stored token ids against the derived
// (YES, NO) pair, writing a 422 and returning false on mismatch.
func (handler *Handler) tokenIDsMatch(w http.ResponseWriter, idx int, market *Market, yes, no *big.Int) bool {
	storedYes := parseTokenID(market.TokenIDYes)
	if storedYes == nil || storedYes.Cmp(yes) != 0 {
		httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("markets[%d].token_id_yes does not match on-chain derivation %s: %s",
				idx, yes.String(), ErrOnChainMismatch.Error()))
		return false
	}
	storedNo := parseTokenID(market.TokenIDNo)
	if storedNo == nil || storedNo.Cmp(no) != 0 {
		httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("markets[%d].token_id_no does not match on-chain derivation %s: %s",
				idx, no.String(), ErrOnChainMismatch.Error()))
		return false
	}
	return true
}

// verifyOutcomeSlotCounts confirms each market's conditionId has been
// prepared on ConditionalTokens with exactly two outcome slots. Writes
// the response and returns false on any failure.
func (handler *Handler) verifyOutcomeSlotCounts(ctx context.Context, w http.ResponseWriter, markets []*Market) bool {
	for idx, market := range markets {
		cid := common.HexToHash(market.ConditionID)
		count, err := handler.ct.OutcomeSlotCount(ctx, cid)
		if err != nil {
			handler.chainError(w, fmt.Sprintf("reading getOutcomeSlotCount for markets[%d]", idx), err)
			return false
		}
		if count == 0 {
			httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("markets[%d]: %s", idx, eth.ErrConditionNotPrepared.Error()))
			return false
		}
		if count != 2 {
			httputil.ErrorResponse(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("markets[%d]: outcome slot count %d != 2", idx, count))
			return false
		}
	}
	return true
}

// finishCreate persists the event + markets and publishes config to KV.
// No Core NATS publish on create — status didn't change from a client's
// perspective; clients refresh listings instead.
func (handler *Handler) finishCreate(ctx context.Context, w http.ResponseWriter, event *Event, markets []*Market) {
	createdEvent, createdMarkets, err := handler.repo.CreateEventWithMarkets(ctx, event, markets)
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

	for _, market := range createdMarkets {
		if err := handler.publisher.PublishMarketConfig(market); err != nil {
			handler.publishFailed(w, "market-config after event creation",
				"event created but config publish failed; please retry", market.ID, err)
			return
		}
	}

	resp := eventWithMarketsResponse{Event: toEventResponse(createdEvent)}
	for _, market := range createdMarkets {
		resp.Markets = append(resp.Markets, toMarketResponse(market))
	}
	_ = httputil.EncodeJSON(w, http.StatusCreated, resp)
}
