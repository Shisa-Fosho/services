package market

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"go.uber.org/zap"
)

// fakeRepo is an in-memory Repository double for handler tests. The methods
// exercised by admin handlers are implemented meaningfully; the rest return
// nil/zero via the embedded Repository interface. Tests can set hooks to
// force errors.
type fakeRepo struct {
	Repository // embed to get default-nil implementations of unused methods
	byID       map[string]*Category
	bySlug     map[string]*Category
	nextID     int

	events  map[string]*Event
	markets map[string]*Market

	createErr error
	updateErr error
	deleteErr error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		byID:    map[string]*Category{},
		bySlug:  map[string]*Category{},
		events:  map[string]*Event{},
		markets: map[string]*Market{},
	}
}

func (f *fakeRepo) putEvent(e *Event) *Event {
	if e.ID == "" {
		f.nextID++
		e.ID = "evt-" + strconv.Itoa(f.nextID)
	}
	stored := *e
	f.events[e.ID] = &stored
	return &stored
}

func (f *fakeRepo) putMarket(m *Market) *Market {
	if m.ID == "" {
		f.nextID++
		m.ID = "mkt-" + strconv.Itoa(f.nextID)
	}
	stored := *m
	f.markets[m.ID] = &stored
	return &stored
}

func (f *fakeRepo) PauseMarkets(_ context.Context, marketIDs []string) ([]*Market, error) {
	if len(marketIDs) == 0 {
		return nil, ErrInvalidMarket
	}
	var missing []string
	for _, id := range marketIDs {
		if _, ok := f.markets[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("markets %v: %w", missing, ErrNotFound)
	}
	// Mirrors PGRepository: already-Paused is an idempotent no-op; any
	// other non-Active status fails the whole batch.
	var invalid []string
	for _, id := range marketIDs {
		if f.markets[id].Status == StatusPaused {
			continue
		}
		if err := ValidateStatusTransition(f.markets[id].Status, StatusPaused); err != nil {
			invalid = append(invalid, id)
		}
	}
	if len(invalid) > 0 {
		return nil, fmt.Errorf("markets not active %v: %w", invalid, ErrInvalidTransition)
	}
	sorted := make([]string, len(marketIDs))
	copy(sorted, marketIDs)
	sort.Strings(sorted)
	out := make([]*Market, 0, len(sorted))
	for _, id := range sorted {
		market := f.markets[id]
		if market.Status != StatusPaused {
			market.Status = StatusPaused
			market.UpdatedAt = time.Now().UTC()
		}
		copyOf := *market
		out = append(out, &copyOf)
	}
	return out, nil
}

func (f *fakeRepo) GetMarket(_ context.Context, id string) (*Market, error) {
	m, ok := f.markets[id]
	if !ok {
		return nil, ErrNotFound
	}
	out := *m
	return &out, nil
}

func (f *fakeRepo) UpdateEvent(_ context.Context, id string, update *EventUpdate) (*Event, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	if err := ValidateEventUpdate(update); err != nil {
		return nil, err
	}
	e, ok := f.events[id]
	if !ok {
		return nil, ErrNotFound
	}
	if update.Title != nil {
		e.Title = *update.Title
	}
	if update.Description != nil {
		e.Description = *update.Description
	}
	if update.CategoryID != nil {
		e.CategoryID = *update.CategoryID
	}
	if update.Featured != nil {
		e.Featured = *update.Featured
	}
	if update.FeaturedSortOrder != nil {
		e.FeaturedSortOrder = *update.FeaturedSortOrder
	}
	return e, nil
}

func (f *fakeRepo) UpdateMarketMetadata(_ context.Context, id string, update *MarketUpdate) (*Market, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	if err := ValidateMarketUpdate(update); err != nil {
		return nil, err
	}
	m, ok := f.markets[id]
	if !ok {
		return nil, ErrNotFound
	}
	if update.Question != nil {
		m.Question = *update.Question
	}
	if update.OutcomeYesLabel != nil {
		m.OutcomeYesLabel = *update.OutcomeYesLabel
	}
	if update.OutcomeNoLabel != nil {
		m.OutcomeNoLabel = *update.OutcomeNoLabel
	}
	return m, nil
}

func (f *fakeRepo) UpdateStatus(_ context.Context, id string, status Status) (*Market, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	m, ok := f.markets[id]
	if !ok {
		return nil, ErrNotFound
	}
	// Mirrors PGRepository: already at the target status is a no-op.
	if m.Status == status {
		return m, nil
	}
	if err := ValidateStatusTransition(m.Status, status); err != nil {
		return nil, err
	}
	m.Status = status
	return m, nil
}

func (f *fakeRepo) UpdateFeeRate(_ context.Context, marketID string, bps int) (*Market, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	if err := ValidateFeeRateBps(bps); err != nil {
		return nil, err
	}
	m, ok := f.markets[marketID]
	if !ok {
		return nil, ErrNotFound
	}
	rate := int64(bps)
	m.FeeRateBps = &rate
	m.UpdatedAt = time.Now().UTC()
	out := *m
	return &out, nil
}

func (f *fakeRepo) UpdateTradingConfig(_ context.Context, marketID string, tickSize TickSize, minSize int64, maxSize *int64) (*Market, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	if err := ValidateTradingConfigUpdate(tickSize, minSize, maxSize); err != nil {
		return nil, err
	}
	m, ok := f.markets[marketID]
	if !ok {
		return nil, ErrNotFound
	}
	m.TickSize = tickSize
	m.MinSize = minSize
	m.MaxSize = maxSize
	m.UpdatedAt = time.Now().UTC()
	out := *m
	return &out, nil
}

func (f *fakeRepo) CreateEventWithMarkets(_ context.Context, event *Event, markets []*Market) (*Event, []*Market, error) {
	if f.createErr != nil {
		return nil, nil, f.createErr
	}
	if len(markets) == 0 {
		return nil, nil, ErrInvalidEvent
	}
	if err := ValidateEvent(event, time.Now()); err != nil {
		return nil, nil, err
	}
	// Check event slug.
	for _, existing := range f.events {
		if existing.Slug == event.Slug {
			return nil, nil, ErrDuplicateSlug
		}
	}
	// Check unique market constraints. (EventID validation happens after
	// the event is inserted — same chicken-and-egg pattern as PGRepository.)
	seenSlug := map[string]bool{}
	seenCondition := map[string]bool{}
	seenQuestion := map[string]bool{}
	for _, market := range markets {
		market.Status = StatusActive
		if seenSlug[market.Slug] || seenCondition[market.ConditionID] || seenQuestion[market.QuestionID] {
			return nil, nil, ErrDuplicateSlug
		}
		seenSlug[market.Slug] = true
		seenCondition[market.ConditionID] = true
		seenQuestion[market.QuestionID] = true
		// Check against existing data.
		for _, existing := range f.markets {
			if existing.Slug == market.Slug || existing.ConditionID == market.ConditionID || existing.QuestionID == market.QuestionID {
				return nil, nil, ErrDuplicateSlug
			}
		}
	}
	if err := ValidateNegRiskCoherence(event, markets); err != nil {
		return nil, nil, err
	}
	storedEvent := f.putEvent(event)
	storedMarkets := make([]*Market, 0, len(markets))
	for _, market := range markets {
		market.EventID = storedEvent.ID
		if err := ValidateMarket(market); err != nil {
			return nil, nil, err
		}
		storedMarkets = append(storedMarkets, f.putMarket(market))
	}
	return storedEvent, storedMarkets, nil
}

func (f *fakeRepo) GetEvent(_ context.Context, id string) (*Event, error) {
	e, ok := f.events[id]
	if !ok {
		return nil, ErrNotFound
	}
	out := *e
	return &out, nil
}

func (f *fakeRepo) ListMarketsByEvent(_ context.Context, eventID string) ([]*Market, error) {
	var out []*Market
	for _, m := range f.markets {
		if m.EventID == eventID {
			cp := *m
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (f *fakeRepo) ResolveMarketsInEvent(_ context.Context, eventID string, outcomes map[string]Outcome) (*Event, []*Market, error) {
	event, ok := f.events[eventID]
	if !ok {
		return nil, nil, ErrNotFound
	}
	if len(outcomes) == 0 {
		return nil, nil, ErrInvalidEvent
	}
	// Verify ownership + transitionability. Mirrors PGRepository:
	// already Resolved with the same outcome is an idempotent no-op;
	// a different outcome is a conflict.
	for marketID, outcome := range outcomes {
		m, ok := f.markets[marketID]
		if !ok || m.EventID != eventID {
			return nil, nil, ErrInvalidMarket
		}
		if m.Status == StatusResolved {
			if m.Outcome == nil || *m.Outcome != outcome {
				return nil, nil, ErrInvalidTransition
			}
			continue
		}
		if m.Status != StatusActive {
			return nil, nil, ErrInvalidTransition
		}
	}
	// Apply.
	for marketID, outcome := range outcomes {
		m := f.markets[marketID]
		m.Status = StatusResolved
		out := outcome
		m.Outcome = &out
	}
	// Auto-flip event status.
	allTerminal := true
	hasResolved := false
	for _, m := range f.markets {
		if m.EventID != eventID {
			continue
		}
		if !m.Status.IsTerminal() {
			allTerminal = false
			break
		}
		if m.Status == StatusResolved {
			hasResolved = true
		}
	}
	if allTerminal {
		if hasResolved {
			event.Status = StatusResolved
		} else {
			event.Status = StatusVoided
		}
	}
	var siblings []*Market
	for _, m := range f.markets {
		if m.EventID == eventID {
			cp := *m
			siblings = append(siblings, &cp)
		}
	}
	eventCopy := *event
	return &eventCopy, siblings, nil
}

func (f *fakeRepo) VoidMarketsInEvent(_ context.Context, eventID string, marketIDs []string) (*Event, []*Market, error) {
	event, ok := f.events[eventID]
	if !ok {
		return nil, nil, ErrNotFound
	}
	if len(marketIDs) == 0 {
		return nil, nil, ErrInvalidEvent
	}
	// Mirrors PGRepository: already-Voided is an idempotent no-op.
	for _, marketID := range marketIDs {
		m, ok := f.markets[marketID]
		if !ok || m.EventID != eventID {
			return nil, nil, ErrInvalidMarket
		}
		if m.Status != StatusActive && m.Status != StatusVoided {
			return nil, nil, ErrInvalidTransition
		}
	}
	for _, marketID := range marketIDs {
		f.markets[marketID].Status = StatusVoided
	}
	allTerminal := true
	hasResolved := false
	for _, m := range f.markets {
		if m.EventID != eventID {
			continue
		}
		if !m.Status.IsTerminal() {
			allTerminal = false
			break
		}
		if m.Status == StatusResolved {
			hasResolved = true
		}
	}
	if allTerminal {
		if hasResolved {
			event.Status = StatusResolved
		} else {
			event.Status = StatusVoided
		}
	}
	var siblings []*Market
	for _, m := range f.markets {
		if m.EventID == eventID {
			cp := *m
			siblings = append(siblings, &cp)
		}
	}
	eventCopy := *event
	return &eventCopy, siblings, nil
}

func (f *fakeRepo) CreateCategory(_ context.Context, cat *Category) error {
	if f.createErr != nil {
		return f.createErr
	}
	if _, exists := f.bySlug[cat.Slug]; exists {
		return ErrDuplicateSlug
	}
	f.nextID++
	cat.ID = "cat-" + strconv.Itoa(f.nextID)
	stored := &Category{ID: cat.ID, Name: cat.Name, Slug: cat.Slug}
	f.byID[cat.ID] = stored
	f.bySlug[cat.Slug] = stored
	return nil
}

func (f *fakeRepo) UpdateCategory(_ context.Context, id, name, slug string) (*Category, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	cat, ok := f.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	if other, exists := f.bySlug[slug]; exists && other.ID != id {
		return nil, ErrDuplicateSlug
	}
	delete(f.bySlug, cat.Slug)
	cat.Name = name
	cat.Slug = slug
	f.bySlug[slug] = cat
	return cat, nil
}

func (f *fakeRepo) DeleteCategory(_ context.Context, id string) error {
	if f.deleteErr != nil {
		return f.deleteErr
	}
	cat, ok := f.byID[id]
	if !ok {
		return ErrNotFound
	}
	delete(f.byID, id)
	delete(f.bySlug, cat.Slug)
	return nil
}

// fakePublisher records publish calls and lets tests force errors on either
// PublishMarketConfig or PublishStatusChange. It satisfies configStore.
// liveStatus seeds LiveStatus responses keyed by market ID; markets absent
// from the map read back as unpublished.
type fakePublisher struct {
	configErr error
	statusErr error

	liveStatus    map[string]string
	liveStatusErr error

	configCalls []*Market
	statusCalls []fakeStatusCall
}

type fakeStatusCall struct {
	MarketID string
	Status   Status
	Outcome  *Outcome
}

func (p *fakePublisher) PublishMarketConfig(market *Market) error {
	if p.configErr != nil {
		return p.configErr
	}
	cp := *market
	p.configCalls = append(p.configCalls, &cp)
	return nil
}

func (p *fakePublisher) PublishStatusChange(_ context.Context, marketID string, status Status) error {
	if p.statusErr != nil {
		return p.statusErr
	}
	p.statusCalls = append(p.statusCalls, fakeStatusCall{MarketID: marketID, Status: status})
	return nil
}

func (p *fakePublisher) PublishStatusChangeWithOutcome(_ context.Context, marketID string, status Status, outcome *Outcome) error {
	if p.statusErr != nil {
		return p.statusErr
	}
	p.statusCalls = append(p.statusCalls, fakeStatusCall{MarketID: marketID, Status: status, Outcome: outcome})
	return nil
}

func (p *fakePublisher) LiveStatus(marketID string) (string, bool, error) {
	if p.liveStatusErr != nil {
		return "", false, p.liveStatusErr
	}
	status, ok := p.liveStatus[marketID]
	return status, ok, nil
}

// fakeChainReader is an in-memory double satisfying eth.CTReader, with
// the negRisk read surface carried alongside (see fakeNegRiskReader —
// both interfaces declare PositionIDs with different signatures, so one
// struct can no longer serve both directly). Tests pre-load expected
// returns keyed by the relevant input; position ids fall back to the
// deterministic fakeTokenPair derivation so request bodies can compute
// matching values via tokenIDsJSON.
type fakeChainReader struct {
	slotCount          map[common.Hash]uint64
	denominator        map[common.Hash]*big.Int
	numerators         map[common.Hash][]*big.Int
	ctPositionIDs      map[common.Hash][2]*big.Int
	negRiskCondIDs     map[common.Hash]common.Hash
	negRiskDetermined  map[common.Hash]bool
	negRiskPositionIDs map[common.Hash][2]*big.Int

	slotCountErr         map[common.Hash]error
	denominatorErr       map[common.Hash]error
	numeratorErr         map[common.Hash]error
	ctPositionIDErr      map[common.Hash]error
	negRiskCondIDErr     map[common.Hash]error
	negRiskDeterminedErr map[common.Hash]error
	negRiskPositionIDErr map[common.Hash]error
}

func newFakeChainReader() *fakeChainReader {
	return &fakeChainReader{
		slotCount:            map[common.Hash]uint64{},
		denominator:          map[common.Hash]*big.Int{},
		numerators:           map[common.Hash][]*big.Int{},
		ctPositionIDs:        map[common.Hash][2]*big.Int{},
		negRiskCondIDs:       map[common.Hash]common.Hash{},
		negRiskDetermined:    map[common.Hash]bool{},
		negRiskPositionIDs:   map[common.Hash][2]*big.Int{},
		slotCountErr:         map[common.Hash]error{},
		denominatorErr:       map[common.Hash]error{},
		numeratorErr:         map[common.Hash]error{},
		ctPositionIDErr:      map[common.Hash]error{},
		negRiskCondIDErr:     map[common.Hash]error{},
		negRiskDeterminedErr: map[common.Hash]error{},
		negRiskPositionIDErr: map[common.Hash]error{},
	}
}

// fakeTokenPair is the deterministic (YES, NO) position id derivation the
// fake readers fall back to when no override is loaded: seeded by the
// conditionId for the CT reader, the questionId for the NegRisk reader.
// seed[1:] keeps the values comfortably inside 256 bits.
func fakeTokenPair(seed common.Hash) (*big.Int, *big.Int) {
	yes := new(big.Int).Lsh(new(big.Int).SetBytes(seed[1:]), 1)
	no := new(big.Int).Add(yes, big.NewInt(1))
	return yes, no
}

// tokenIDsJSON renders the token_id_yes/token_id_no body fields matching
// fakeTokenPair for the given seed hex (conditionId for BINARY markets,
// questionId for NEG_RISK markets).
func tokenIDsJSON(seedHex string) string {
	yes, no := fakeTokenPair(common.HexToHash(seedHex))
	return `"token_id_yes":"` + yes.String() + `","token_id_no":"` + no.String() + `"`
}

func (f *fakeChainReader) OutcomeSlotCount(_ context.Context, conditionID common.Hash) (uint64, error) {
	if err, ok := f.slotCountErr[conditionID]; ok {
		return 0, err
	}
	return f.slotCount[conditionID], nil
}

func (f *fakeChainReader) PayoutDenominator(_ context.Context, conditionID common.Hash) (*big.Int, error) {
	if err, ok := f.denominatorErr[conditionID]; ok {
		return nil, err
	}
	if value, ok := f.denominator[conditionID]; ok {
		return new(big.Int).Set(value), nil
	}
	return new(big.Int), nil
}

func (f *fakeChainReader) PayoutNumerators(_ context.Context, conditionID common.Hash, slotCount uint64) ([]*big.Int, error) {
	if err, ok := f.numeratorErr[conditionID]; ok {
		return nil, err
	}
	nums, ok := f.numerators[conditionID]
	if !ok {
		out := make([]*big.Int, slotCount)
		for idx := range out {
			out[idx] = new(big.Int)
		}
		return out, nil
	}
	out := make([]*big.Int, slotCount)
	for idx := uint64(0); idx < slotCount; idx++ {
		if int(idx) < len(nums) && nums[idx] != nil {
			out[idx] = new(big.Int).Set(nums[idx])
		} else {
			out[idx] = new(big.Int)
		}
	}
	return out, nil
}

// PositionIDs satisfies eth.CTReader (binary token derivation, keyed by
// conditionId; the collateral address is irrelevant to the fake).
func (f *fakeChainReader) PositionIDs(_ context.Context, _ common.Address, conditionID common.Hash) (*big.Int, *big.Int, error) {
	if err, ok := f.ctPositionIDErr[conditionID]; ok {
		return nil, nil, err
	}
	if pair, ok := f.ctPositionIDs[conditionID]; ok {
		return pair[0], pair[1], nil
	}
	yes, no := fakeTokenPair(conditionID)
	return yes, no, nil
}

// fakeNegRiskReader adapts fakeChainReader to eth.NegRiskReader. Its
// PositionIDs (keyed by questionId) shadows the embedded CT-signature
// method — the two interfaces diverged when token derivation was added.
type fakeNegRiskReader struct{ *fakeChainReader }

// PositionIDs satisfies eth.NegRiskReader.
func (f fakeNegRiskReader) PositionIDs(_ context.Context, questionID common.Hash) (*big.Int, *big.Int, error) {
	if err, ok := f.negRiskPositionIDErr[questionID]; ok {
		return nil, nil, err
	}
	if pair, ok := f.negRiskPositionIDs[questionID]; ok {
		return pair[0], pair[1], nil
	}
	yes, no := fakeTokenPair(questionID)
	return yes, no, nil
}

// ConditionID satisfies negRiskReader.
func (f *fakeChainReader) ConditionID(_ context.Context, questionID common.Hash) (common.Hash, error) {
	if err, ok := f.negRiskCondIDErr[questionID]; ok {
		return common.Hash{}, err
	}
	return f.negRiskCondIDs[questionID], nil
}

// MarketDetermined satisfies negRiskReader.
func (f *fakeChainReader) MarketDetermined(_ context.Context, marketID common.Hash) (bool, error) {
	if err, ok := f.negRiskDeterminedErr[marketID]; ok {
		return false, err
	}
	return f.negRiskDetermined[marketID], nil
}

// testCollateral is the collateral token address handlers are
// constructed with in tests — an arbitrary fixed value, since the fake
// CT reader ignores it when deriving position ids.
var testCollateral = common.HexToAddress("0x00000000000000000000000000000000000000CC")

func newHandlerForTest(t *testing.T, repo Repository) *Handler {
	t.Helper()
	logger := zap.NewNop()
	chain := newFakeChainReader()
	return NewHandler(repo, &fakePublisher{}, chain, fakeNegRiskReader{chain}, testCollateral, logger)
}

// newHandlerWithPublisher is like newHandlerForTest but lets the caller
// inject a fakePublisher with pre-set error hooks.
func newHandlerWithPublisher(t *testing.T, repo Repository, publisher configStore) *Handler {
	t.Helper()
	logger := zap.NewNop()
	chain := newFakeChainReader()
	return NewHandler(repo, publisher, chain, fakeNegRiskReader{chain}, testCollateral, logger)
}

// newHandlerWithChain injects both a publisher and a chain reader,
// used by tests that need to pre-load on-chain state.
func newHandlerWithChain(t *testing.T, repo Repository, publisher configStore, chain *fakeChainReader) *Handler {
	t.Helper()
	logger := zap.NewNop()
	return NewHandler(repo, publisher, chain, fakeNegRiskReader{chain}, testCollateral, logger)
}

// passThroughAdmin is an "admin middleware" stand-in for handler tests:
// it just calls next. The real admin check is exercised in auth/middleware_test.go.
func passThroughAdmin(next http.Handler) http.Handler { return next }

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	buf, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return buf
}

func doRequest(t *testing.T, h http.Handler, method, target string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, target, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, target, nil)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func registeredMux(t *testing.T, repo Repository) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	h := newHandlerForTest(t, repo)
	h.RegisterAdminRoutes(mux, passThroughAdmin)
	return mux
}

func TestHandler_CreateCategory_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	mux := registeredMux(t, repo)

	body := mustJSON(t, categoryRequest{Name: "Sports", Slug: "sports"})
	rec := doRequest(t, mux, http.MethodPost, "/admin/categories", body)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%q, want 201", rec.Code, rec.Body.String())
	}
	var got categoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ID == "" {
		t.Error("expected non-empty id in response")
	}
	if got.Name != "Sports" || got.Slug != "sports" {
		t.Errorf("got = %+v", got)
	}
}

func TestHandler_CreateCategory_DuplicateSlug(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	// pre-seed
	_ = repo.CreateCategory(context.Background(), &Category{Name: "Sports", Slug: "sports"})
	mux := registeredMux(t, repo)

	body := mustJSON(t, categoryRequest{Name: "Sports Again", Slug: "sports"})
	rec := doRequest(t, mux, http.MethodPost, "/admin/categories", body)

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

func TestHandler_CreateCategory_MissingFields(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	mux := registeredMux(t, repo)

	cases := []struct{ name, slug string }{
		{"", "sports"},
		{"Sports", ""},
		{"", ""},
	}
	for _, tc := range cases {
		body := mustJSON(t, categoryRequest{Name: tc.name, Slug: tc.slug})
		rec := doRequest(t, mux, http.MethodPost, "/admin/categories", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("name=%q slug=%q: status = %d, want 400", tc.name, tc.slug, rec.Code)
		}
	}
}

func TestHandler_CreateCategory_MalformedJSON(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	mux := registeredMux(t, repo)

	rec := doRequest(t, mux, http.MethodPost, "/admin/categories", []byte("{not valid"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandler_CreateCategory_RepoError(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	repo.createErr = errors.New("db down")
	mux := registeredMux(t, repo)

	body := mustJSON(t, categoryRequest{Name: "Sports", Slug: "sports"})
	rec := doRequest(t, mux, http.MethodPost, "/admin/categories", body)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

func TestHandler_UpdateCategory_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := &Category{Name: "Sports", Slug: "sports"}
	_ = repo.CreateCategory(context.Background(), seed)
	mux := registeredMux(t, repo)

	body := mustJSON(t, categoryRequest{Name: "Sports & Leisure", Slug: "sports-leisure"})
	rec := doRequest(t, mux, http.MethodPut, "/admin/categories/"+seed.ID, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", rec.Code, rec.Body.String())
	}
	var got categoryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != seed.ID {
		t.Errorf("id = %q, want %q", got.ID, seed.ID)
	}
	if got.Name != "Sports & Leisure" || got.Slug != "sports-leisure" {
		t.Errorf("got = %+v", got)
	}
}

func TestHandler_UpdateCategory_NotFound(t *testing.T) {
	t.Parallel()
	mux := registeredMux(t, newFakeRepo())

	body := mustJSON(t, categoryRequest{Name: "Ghost", Slug: "ghost"})
	rec := doRequest(t, mux, http.MethodPut, "/admin/categories/missing-id", body)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandler_UpdateCategory_DuplicateSlug(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	first := &Category{Name: "Sports", Slug: "sports"}
	second := &Category{Name: "Politics", Slug: "politics"}
	_ = repo.CreateCategory(context.Background(), first)
	_ = repo.CreateCategory(context.Background(), second)
	mux := registeredMux(t, repo)

	body := mustJSON(t, categoryRequest{Name: "Politics", Slug: "sports"})
	rec := doRequest(t, mux, http.MethodPut, "/admin/categories/"+second.ID, body)

	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

func TestHandler_DeleteCategory_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	cat := &Category{Name: "Sports", Slug: "sports"}
	_ = repo.CreateCategory(context.Background(), cat)
	mux := registeredMux(t, repo)

	rec := doRequest(t, mux, http.MethodDelete, "/admin/categories/"+cat.ID, nil)

	if rec.Code != http.StatusNoContent {
		t.Errorf("status = %d body=%q, want 204", rec.Code, rec.Body.String())
	}
	if rec.Body.Len() != 0 {
		t.Errorf("expected empty body, got %q", rec.Body.String())
	}
}

func TestHandler_DeleteCategory_NotFound(t *testing.T) {
	t.Parallel()
	mux := registeredMux(t, newFakeRepo())

	rec := doRequest(t, mux, http.MethodDelete, "/admin/categories/missing-id", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandler_UpdateEvent_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putEvent(&Event{
		Title: "Original", Description: "desc",
		CategoryID: "cat-original", Featured: false,
	})
	mux := registeredMux(t, repo)

	newTitle := "Updated"
	featured := true
	body := mustJSON(t, eventUpdateRequest{Title: &newTitle, Featured: &featured})
	rec := doRequest(t, mux, http.MethodPut, "/admin/events/"+seed.ID, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", rec.Code, rec.Body.String())
	}
	var got eventResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Title != "Updated" {
		t.Errorf("title = %q, want Updated", got.Title)
	}
	if !got.Featured {
		t.Error("featured = false, want true")
	}
	if got.Description != "desc" {
		t.Errorf("description unexpectedly changed: %q", got.Description)
	}
	if got.CategoryID != "cat-original" {
		t.Errorf("category unexpectedly changed: %q", got.CategoryID)
	}
}

func TestHandler_UpdateEvent_ChangeCategory(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putEvent(&Event{Title: "E", CategoryID: "cat-a"})
	mux := registeredMux(t, repo)

	newCat := "cat-b"
	body := mustJSON(t, eventUpdateRequest{CategoryID: &newCat})
	rec := doRequest(t, mux, http.MethodPut, "/admin/events/"+seed.ID, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", rec.Code, rec.Body.String())
	}
	var got eventResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.CategoryID != "cat-b" {
		t.Errorf("category_id = %q, want cat-b", got.CategoryID)
	}
}

func TestHandler_UpdateEvent_EmptyCategoryRejected(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putEvent(&Event{Title: "E", CategoryID: "cat-a"})
	mux := registeredMux(t, repo)

	empty := ""
	body := mustJSON(t, eventUpdateRequest{CategoryID: &empty})
	rec := doRequest(t, mux, http.MethodPut, "/admin/events/"+seed.ID, body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 (category cannot be cleared)", rec.Code)
	}
}

func TestHandler_UpdateEvent_NotFound(t *testing.T) {
	t.Parallel()
	mux := registeredMux(t, newFakeRepo())

	title := "x"
	body := mustJSON(t, eventUpdateRequest{Title: &title})
	rec := doRequest(t, mux, http.MethodPut, "/admin/events/missing-id", body)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandler_UpdateEvent_EmptyBody(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putEvent(&Event{Title: "E"})
	mux := registeredMux(t, repo)

	body := mustJSON(t, eventUpdateRequest{})
	rec := doRequest(t, mux, http.MethodPut, "/admin/events/"+seed.ID, body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for empty update", rec.Code)
	}
}

func TestHandler_UpdateMarket_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putMarket(&Market{
		Question: "Old?", OutcomeYesLabel: "Yes", OutcomeNoLabel: "No",
		Status: StatusActive, PriceYes: 50, PriceNo: 50,
	})
	mux := registeredMux(t, repo)

	newQ := "New?"
	body := mustJSON(t, marketUpdateRequest{Question: &newQ})
	rec := doRequest(t, mux, http.MethodPut, "/admin/markets/"+seed.ID, body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", rec.Code, rec.Body.String())
	}
	var got marketResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Question != "New?" {
		t.Errorf("question = %q, want New?", got.Question)
	}
	if got.OutcomeYesLabel != "Yes" {
		t.Errorf("yes label unexpectedly changed: %q", got.OutcomeYesLabel)
	}
}

func TestHandler_UpdateMarket_NotFound(t *testing.T) {
	t.Parallel()
	mux := registeredMux(t, newFakeRepo())

	q := "x"
	body := mustJSON(t, marketUpdateRequest{Question: &q})
	rec := doRequest(t, mux, http.MethodPut, "/admin/markets/missing-id", body)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandler_PauseMarket_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putMarket(&Market{Question: "Q?", Status: StatusActive})
	mux := registeredMux(t, repo)

	rec := doRequest(t, mux, http.MethodPost, "/admin/markets/"+seed.ID+"/pause", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", rec.Code, rec.Body.String())
	}
	var got marketResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != "PAUSED" {
		t.Errorf("status = %q, want PAUSED", got.Status)
	}
}

func TestHandler_ResumeMarket_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putMarket(&Market{Question: "Q?", Status: StatusPaused})
	mux := registeredMux(t, repo)

	rec := doRequest(t, mux, http.MethodPost, "/admin/markets/"+seed.ID+"/resume", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", rec.Code, rec.Body.String())
	}
	var got marketResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != "ACTIVE" {
		t.Errorf("status = %q, want ACTIVE", got.Status)
	}
}

func TestHandler_PauseMarket_InvalidTransition(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putMarket(&Market{Question: "Q?", Status: StatusResolved})
	mux := registeredMux(t, repo)

	rec := doRequest(t, mux, http.MethodPost, "/admin/markets/"+seed.ID+"/pause", nil)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

func TestHandler_PauseMarket_NotFound(t *testing.T) {
	t.Parallel()
	mux := registeredMux(t, newFakeRepo())

	rec := doRequest(t, mux, http.MethodPost, "/admin/markets/missing-id/pause", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandler_SetFeeRate_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putMarket(&Market{Question: "Q?", Status: StatusActive})
	mux := registeredMux(t, repo)

	body := mustJSON(t, feeRateRequest{FeeRateBps: 50})
	rec := doRequest(t, mux, http.MethodPut, "/admin/markets/"+seed.ID+"/fee-rate", body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", rec.Code, rec.Body.String())
	}
	var got feeRateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.MarketID != seed.ID {
		t.Errorf("market_id = %q, want %q", got.MarketID, seed.ID)
	}
	if got.FeeRateBps != 50 {
		t.Errorf("fee_rate_bps = %d, want 50", got.FeeRateBps)
	}
}

func TestHandler_SetFeeRate_OutOfRange(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putMarket(&Market{Question: "Q?", Status: StatusActive})
	mux := registeredMux(t, repo)

	cases := []feeRateRequest{
		{FeeRateBps: -1},
		{FeeRateBps: MaxFeeBps + 1},
		{FeeRateBps: 5000},
	}
	for _, tc := range cases {
		body := mustJSON(t, tc)
		rec := doRequest(t, mux, http.MethodPut, "/admin/markets/"+seed.ID+"/fee-rate", body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("bps=%d: status = %d, want 400", tc.FeeRateBps, rec.Code)
		}
	}
}

func TestHandler_SetFeeRate_MarketNotFound(t *testing.T) {
	t.Parallel()
	mux := registeredMux(t, newFakeRepo())

	body := mustJSON(t, feeRateRequest{FeeRateBps: 50})
	rec := doRequest(t, mux, http.MethodPut, "/admin/markets/missing-id/fee-rate", body)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandler_SetFeeRate_MalformedJSON(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putMarket(&Market{Question: "Q?", Status: StatusActive})
	mux := registeredMux(t, repo)

	rec := doRequest(t, mux, http.MethodPut, "/admin/markets/"+seed.ID+"/fee-rate", []byte("{not valid"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandler_SetFeeRate_RepoError(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	repo.updateErr = errors.New("db down")
	mux := registeredMux(t, repo)

	body := mustJSON(t, feeRateRequest{FeeRateBps: 50})
	rec := doRequest(t, mux, http.MethodPut, "/admin/markets/mkt-1/fee-rate", body)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// muxWithPublisher mirrors registeredMux but lets the caller substitute a
// fakePublisher with pre-set error hooks, for tests that exercise the
// pause/resume/metadata publishing paths.
func muxWithPublisher(t *testing.T, repo Repository, publisher configStore) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	h := newHandlerWithPublisher(t, repo, publisher)
	h.RegisterAdminRoutes(mux, passThroughAdmin)
	return mux
}

func TestHandler_PauseMarket_PublishesConfigAndStatus(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putMarket(&Market{Question: "Q?", Status: StatusActive, TokenIDYes: "y", TokenIDNo: "n"})
	pub := &fakePublisher{}
	mux := muxWithPublisher(t, repo, pub)

	rec := doRequest(t, mux, http.MethodPost, "/admin/markets/"+seed.ID+"/pause", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", rec.Code, rec.Body.String())
	}
	if len(pub.configCalls) != 1 {
		t.Fatalf("PublishMarketConfig called %d times, want 1", len(pub.configCalls))
	}
	if pub.configCalls[0].Status != StatusPaused {
		t.Errorf("config call market status = %s, want PAUSED", pub.configCalls[0].Status)
	}
	if len(pub.statusCalls) != 1 {
		t.Fatalf("PublishStatusChange called %d times, want 1", len(pub.statusCalls))
	}
	if pub.statusCalls[0].Status != StatusPaused {
		t.Errorf("status call status = %s, want PAUSED", pub.statusCalls[0].Status)
	}
}

func TestHandler_BulkPauseMarkets_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	m1 := repo.putMarket(&Market{Question: "Q1", Status: StatusActive, TokenIDYes: "y1", TokenIDNo: "n1"})
	m2 := repo.putMarket(&Market{Question: "Q2", Status: StatusActive, TokenIDYes: "y2", TokenIDNo: "n2"})
	m3 := repo.putMarket(&Market{Question: "Q3", Status: StatusActive, TokenIDYes: "y3", TokenIDNo: "n3"})
	pub := &fakePublisher{}
	mux := muxWithPublisher(t, repo, pub)

	body := mustJSON(t, bulkPauseRequest{MarketIDs: []string{m1.ID, m2.ID, m3.ID}})
	rec := doRequest(t, mux, http.MethodPost, "/admin/markets/bulk-pause", body)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", rec.Code, rec.Body.String())
	}
	if repo.markets[m1.ID].Status != StatusPaused ||
		repo.markets[m2.ID].Status != StatusPaused ||
		repo.markets[m3.ID].Status != StatusPaused {
		t.Errorf("not all markets paused: m1=%s m2=%s m3=%s",
			repo.markets[m1.ID].Status, repo.markets[m2.ID].Status, repo.markets[m3.ID].Status)
	}
	if len(pub.configCalls) != 3 {
		t.Errorf("config publishes = %d, want 3", len(pub.configCalls))
	}
	if len(pub.statusCalls) != 3 {
		t.Errorf("status publishes = %d, want 3", len(pub.statusCalls))
	}
	for _, call := range pub.statusCalls {
		if call.Status != StatusPaused {
			t.Errorf("status call status = %s, want PAUSED", call.Status)
		}
	}
}

func TestHandler_BulkPauseMarkets_EmptyList(t *testing.T) {
	t.Parallel()
	mux := muxWithPublisher(t, newFakeRepo(), &fakePublisher{})

	body := mustJSON(t, bulkPauseRequest{MarketIDs: []string{}})
	rec := doRequest(t, mux, http.MethodPost, "/admin/markets/bulk-pause", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandler_BulkPauseMarkets_OneNotFound_FailsAllOrNothing(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	m1 := repo.putMarket(&Market{Question: "Q1", Status: StatusActive, TokenIDYes: "y", TokenIDNo: "n"})
	pub := &fakePublisher{}
	mux := muxWithPublisher(t, repo, pub)

	body := mustJSON(t, bulkPauseRequest{MarketIDs: []string{m1.ID, "ghost-id"}})
	rec := doRequest(t, mux, http.MethodPost, "/admin/markets/bulk-pause", body)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
	// All-or-nothing: m1 must NOT have been paused.
	if repo.markets[m1.ID].Status != StatusActive {
		t.Errorf("m1 status = %s, want ACTIVE (batch must roll back)", repo.markets[m1.ID].Status)
	}
	if len(pub.configCalls) != 0 {
		t.Errorf("config calls = %d, want 0 (no publishes on failed batch)", len(pub.configCalls))
	}
}

func TestHandler_BulkPauseMarkets_OneAlreadyPaused_IsIdempotent(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	m1 := repo.putMarket(&Market{Question: "Q1", Status: StatusActive, TokenIDYes: "y", TokenIDNo: "n"})
	m2 := repo.putMarket(&Market{Question: "Q2", Status: StatusPaused, TokenIDYes: "y", TokenIDNo: "n"})
	pub := &fakePublisher{}
	mux := muxWithPublisher(t, repo, pub)

	body := mustJSON(t, bulkPauseRequest{MarketIDs: []string{m1.ID, m2.ID}})
	rec := doRequest(t, mux, http.MethodPost, "/admin/markets/bulk-pause", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200 (already-paused is a no-op)", rec.Code, rec.Body.String())
	}
	if repo.markets[m1.ID].Status != StatusPaused {
		t.Errorf("m1 status = %s, want PAUSED", repo.markets[m1.ID].Status)
	}
	// Both markets republish so a retry after a failed publish heals KV.
	if len(pub.configCalls) != 2 {
		t.Errorf("config calls = %d, want 2", len(pub.configCalls))
	}
}

func TestHandler_BulkPauseMarkets_OneResolved_FailsAllOrNothing(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	m1 := repo.putMarket(&Market{Question: "Q1", Status: StatusActive, TokenIDYes: "y", TokenIDNo: "n"})
	m2 := repo.putMarket(&Market{Question: "Q2", Status: StatusResolved, TokenIDYes: "y", TokenIDNo: "n"})
	pub := &fakePublisher{}
	mux := muxWithPublisher(t, repo, pub)

	body := mustJSON(t, bulkPauseRequest{MarketIDs: []string{m1.ID, m2.ID}})
	rec := doRequest(t, mux, http.MethodPost, "/admin/markets/bulk-pause", body)
	if rec.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", rec.Code)
	}
	if repo.markets[m1.ID].Status != StatusActive {
		t.Errorf("m1 status = %s, want ACTIVE (resolved m2 must not partially commit)",
			repo.markets[m1.ID].Status)
	}
	if len(pub.configCalls) != 0 {
		t.Errorf("config calls = %d, want 0 (no publishes on failed batch)", len(pub.configCalls))
	}
}

func TestHandler_BulkPauseMarkets_KVPublishFailureReturns502(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	m1 := repo.putMarket(&Market{Question: "Q1", Status: StatusActive, TokenIDYes: "y", TokenIDNo: "n"})
	pub := &fakePublisher{configErr: errors.New("KV down")}
	mux := muxWithPublisher(t, repo, pub)

	body := mustJSON(t, bulkPauseRequest{MarketIDs: []string{m1.ID}})
	rec := doRequest(t, mux, http.MethodPost, "/admin/markets/bulk-pause", body)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
	// DB write committed before publish — m1 is paused even though publish failed.
	if repo.markets[m1.ID].Status != StatusPaused {
		t.Errorf("m1 status = %s, want PAUSED — DB commit must succeed before KV publish",
			repo.markets[m1.ID].Status)
	}
}

func TestHandler_PauseMarket_KVPublishFailureReturns502(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putMarket(&Market{Question: "Q?", Status: StatusActive, TokenIDYes: "y", TokenIDNo: "n"})
	pub := &fakePublisher{configErr: errors.New("KV down")}
	mux := muxWithPublisher(t, repo, pub)

	rec := doRequest(t, mux, http.MethodPost, "/admin/markets/"+seed.ID+"/pause", nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	// DB write must still have committed — the market must be paused.
	if repo.markets[seed.ID].Status != StatusPaused {
		t.Errorf("market.Status = %s, want PAUSED — DB commit must succeed before KV publish",
			repo.markets[seed.ID].Status)
	}
	// Status-change broadcast must NOT have been attempted: KV is the
	// authoritative state for trading; if it's stale, broadcasting "paused"
	// to WebSocket clients would mislead them.
	if len(pub.statusCalls) != 0 {
		t.Errorf("PublishStatusChange called %d times after KV failure, want 0", len(pub.statusCalls))
	}
}

func TestHandler_PauseMarket_StatusPublishFailureReturns502(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putMarket(&Market{Question: "Q?", Status: StatusActive, TokenIDYes: "y", TokenIDNo: "n"})
	pub := &fakePublisher{statusErr: errors.New("NATS down")}
	mux := muxWithPublisher(t, repo, pub)

	rec := doRequest(t, mux, http.MethodPost, "/admin/markets/"+seed.ID+"/pause", nil)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	// KV publish ran and succeeded — the failure was the second-stage broadcast.
	if len(pub.configCalls) != 1 {
		t.Errorf("PublishMarketConfig called %d times, want 1", len(pub.configCalls))
	}
}

func TestHandler_GetMarket_ReturnsQuestionIDAndTradingConfig(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	maxSize := int64(1000)
	feeRate := int64(50)
	seed := repo.putMarket(&Market{
		Question:    "Will it?",
		Status:      StatusActive,
		TokenIDYes:  "yes-tok",
		TokenIDNo:   "no-tok",
		ConditionID: fixedCondHash("cond"),
		QuestionID:  fixedCondHash("question"),
		TickSize:    TickSize0_01,
		MinSize:     5,
		MaxSize:     &maxSize,
		FeeRateBps:  &feeRate,
	})
	mux := registeredMux(t, repo)

	rec := doRequest(t, mux, http.MethodGet, "/admin/markets/"+seed.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", rec.Code, rec.Body.String())
	}
	var got marketResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.QuestionID != seed.QuestionID {
		t.Errorf("question_id = %q, want %q", got.QuestionID, seed.QuestionID)
	}
	if got.TickSize != TickSize0_01.String() {
		t.Errorf("tick_size = %q, want %q", got.TickSize, TickSize0_01.String())
	}
	if got.MinSize != 5 {
		t.Errorf("min_size = %d, want 5", got.MinSize)
	}
	if got.MaxSize == nil || *got.MaxSize != maxSize {
		t.Errorf("max_size = %v, want %d", got.MaxSize, maxSize)
	}
	if got.FeeRateBps == nil || *got.FeeRateBps != feeRate {
		t.Errorf("fee_rate_bps = %v, want %d", got.FeeRateBps, feeRate)
	}
}

func TestHandler_GetMarket_NotFound(t *testing.T) {
	t.Parallel()
	mux := registeredMux(t, newFakeRepo())

	rec := doRequest(t, mux, http.MethodGet, "/admin/markets/no-such-id", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandler_UpdateMarket_KVPublishFailureReturns502(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putMarket(&Market{Question: "Old?", Status: StatusActive, TokenIDYes: "y", TokenIDNo: "n"})
	pub := &fakePublisher{configErr: errors.New("KV down")}
	mux := muxWithPublisher(t, repo, pub)

	newQ := "New?"
	body := mustJSON(t, marketUpdateRequest{Question: &newQ})
	rec := doRequest(t, mux, http.MethodPut, "/admin/markets/"+seed.ID, body)

	// Telling the user "saved" when downstream cache wasn't updated is a lie.
	// Without an outbox/reconciliation worker, a missed publish leaves trading
	// reading stale config indefinitely — fail loudly so the admin retries.
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestHandler_SetFeeRate_PublishesConfig(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putMarket(&Market{Question: "Q?", Status: StatusActive, TokenIDYes: "y", TokenIDNo: "n"})
	pub := &fakePublisher{}
	mux := muxWithPublisher(t, repo, pub)

	body := mustJSON(t, feeRateRequest{FeeRateBps: 50})
	rec := doRequest(t, mux, http.MethodPut, "/admin/markets/"+seed.ID+"/fee-rate", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", rec.Code, rec.Body.String())
	}
	if len(pub.configCalls) != 1 {
		t.Fatalf("PublishMarketConfig called %d times, want 1", len(pub.configCalls))
	}
}

func TestHandler_SetFeeRate_KVPublishFailureReturns502(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putMarket(&Market{Question: "Q?", Status: StatusActive, TokenIDYes: "y", TokenIDNo: "n"})
	pub := &fakePublisher{configErr: errors.New("KV down")}
	mux := muxWithPublisher(t, repo, pub)

	body := mustJSON(t, feeRateRequest{FeeRateBps: 50})
	rec := doRequest(t, mux, http.MethodPut, "/admin/markets/"+seed.ID+"/fee-rate", body)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

func TestHandler_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	mux := registeredMux(t, newFakeRepo())

	// GET /admin/categories is not registered; stdlib 1.22 mux returns 405
	// because POST is registered for that exact path.
	rec := doRequest(t, mux, http.MethodGet, "/admin/categories", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

// --- helpers for new endpoint tests --------------------------------------

// mkBigInt is a one-liner for big.NewInt(int64(n)).
func mkBigInt(n int) *big.Int { return big.NewInt(int64(n)) }

// muxWithChain wires a handler with caller-supplied publisher + chain.
func muxWithChain(t *testing.T, repo Repository, publisher configStore, chain *fakeChainReader) *http.ServeMux {
	t.Helper()
	mux := http.NewServeMux()
	h := newHandlerWithChain(t, repo, publisher, chain)
	h.RegisterAdminRoutes(mux, passThroughAdmin)
	return mux
}

// seedCatID adds a category to a fakeRepo and returns its id.
func seedCatID(t *testing.T, repo *fakeRepo, slug string) string {
	t.Helper()
	cat := &Category{Name: slug, Slug: slug}
	if err := repo.CreateCategory(context.Background(), cat); err != nil {
		t.Fatalf("seed category: %v", err)
	}
	return cat.ID
}

// fixedCondHash is a stable, well-formed bytes32 hex for tests that don't
// care about the actual identifier. The slug is hex-encoded so any input
// yields a valid 0x-prefixed 64-hex-char string (the create handlers
// reject malformed hex at the boundary).
func fixedCondHash(slug string) string {
	encoded := hex.EncodeToString([]byte(slug))
	for len(encoded) < 64 {
		encoded += "0"
	}
	return "0x" + encoded[:64]
}

// negRiskQuestionID derives the questionId for a NegRisk marketId and
// question index: first 31 bytes shared with the marketId, final byte =
// index (NegRiskIdLib layout, matching the create handler's coherence
// check).
func negRiskQuestionID(marketIDHex string, index byte) string {
	id := common.HexToHash(marketIDHex)
	id[31] = index
	return id.Hex()
}

// negRiskMarketIDHex builds a well-formed adapter marketId (final byte
// zero) from a slug.
func negRiskMarketIDHex(slug string) string {
	id := common.HexToHash(fixedCondHash(slug))
	id[31] = 0
	return id.Hex()
}

func binaryEventBody(slug, catID, condHex, questionHex string) []byte {
	return []byte(`{
		"slug":"` + slug + `","title":"T","description":"D",
		"category_id":"` + catID + `",
		"end_date":"` + time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339) + `",
		"markets":[{
			"slug":"` + slug + `-m1","question":"Q?",
			"outcome_yes_label":"Yes","outcome_no_label":"No",
			` + tokenIDsJSON(condHex) + `,
			"condition_id":"` + condHex + `","question_id":"` + questionHex + `",
			"tick_size":"0.01","min_size":5
		}]
	}`)
}

// --- /admin/events POST (BINARY) ----------------------------------------

func TestHandler_CreateEvent_Binary_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "politics")
	pub := &fakePublisher{}
	chain := newFakeChainReader()
	cond := fixedCondHash("c1")
	chain.slotCount[common.HexToHash(cond)] = 2
	mux := muxWithChain(t, repo, pub, chain)

	rec := doRequest(t, mux, http.MethodPost, "/admin/events/binary",
		binaryEventBody("election", catID, cond, fixedCondHash("q1")))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%q, want 201", rec.Code, rec.Body.String())
	}
	if len(pub.configCalls) != 1 {
		t.Errorf("PublishMarketConfig calls = %d, want 1", len(pub.configCalls))
	}
	// Response should include question_id round-tripped.
	var resp eventWithMarketsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Markets) != 1 {
		t.Fatalf("markets in response = %d, want 1", len(resp.Markets))
	}
	if resp.Markets[0].QuestionID == "" {
		t.Error("response.markets[0].question_id is empty; want round-tripped value")
	}
}

func TestHandler_CreateEvent_Binary_MissingQuestionID(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "politics")
	pub := &fakePublisher{}
	chain := newFakeChainReader()
	mux := muxWithChain(t, repo, pub, chain)

	body := []byte(`{
		"slug":"e1","title":"T","description":"D",
		"category_id":"` + catID + `",
		"end_date":"` + time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339) + `",
		"markets":[{
			"slug":"e1-m1","question":"Q?",
			"outcome_yes_label":"Yes","outcome_no_label":"No",
			` + tokenIDsJSON(fixedCondHash("c")) + `,
			"condition_id":"` + fixedCondHash("c") + `",
			"tick_size":"0.01","min_size":5
		}]
	}`)
	rec := doRequest(t, mux, http.MethodPost, "/admin/events/binary", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandler_CreateEvent_Binary_EmptyMarkets(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	pub := &fakePublisher{}
	chain := newFakeChainReader()
	mux := muxWithChain(t, repo, pub, chain)

	body := []byte(`{
		"slug":"empty","title":"T","description":"D",
		"category_id":"` + catID + `",
		"end_date":"` + time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339) + `",
		"markets":[]
	}`)
	rec := doRequest(t, mux, http.MethodPost, "/admin/events/binary", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandler_CreateEvent_Binary_OnChainNotPrepared(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	pub := &fakePublisher{}
	chain := newFakeChainReader()
	// Don't seed slotCount → returns 0.
	mux := muxWithChain(t, repo, pub, chain)

	rec := doRequest(t, mux, http.MethodPost, "/admin/events/binary",
		binaryEventBody("notprep", catID, fixedCondHash("c2"), fixedCondHash("q2")))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
}

func TestHandler_CreateEvent_Binary_SlotCountMismatch(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	pub := &fakePublisher{}
	chain := newFakeChainReader()
	cond := fixedCondHash("c3")
	chain.slotCount[common.HexToHash(cond)] = 3
	mux := muxWithChain(t, repo, pub, chain)

	rec := doRequest(t, mux, http.MethodPost, "/admin/events/binary",
		binaryEventBody("slot-mismatch", catID, cond, fixedCondHash("q3")))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
}

func TestHandler_CreateEvent_Binary_MultiMarket(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "politics")
	pub := &fakePublisher{}
	chain := newFakeChainReader()
	c1 := fixedCondHash("c1")
	c2 := fixedCondHash("c2")
	c3 := fixedCondHash("c3")
	chain.slotCount[common.HexToHash(c1)] = 2
	chain.slotCount[common.HexToHash(c2)] = 2
	chain.slotCount[common.HexToHash(c3)] = 2
	mux := muxWithChain(t, repo, pub, chain)

	body := []byte(`{
		"slug":"multi","title":"T","description":"D",
		"category_id":"` + catID + `",
		"end_date":"` + time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339) + `",
		"markets":[
			{"slug":"m1","question":"Q1?","outcome_yes_label":"Yes","outcome_no_label":"No",` + tokenIDsJSON(c1) + `,"condition_id":"` + c1 + `","question_id":"` + fixedCondHash("q1") + `","tick_size":"0.01","min_size":5},
			{"slug":"m2","question":"Q2?","outcome_yes_label":"Yes","outcome_no_label":"No",` + tokenIDsJSON(c2) + `,"condition_id":"` + c2 + `","question_id":"` + fixedCondHash("q2") + `","tick_size":"0.01","min_size":5},
			{"slug":"m3","question":"Q3?","outcome_yes_label":"Yes","outcome_no_label":"No",` + tokenIDsJSON(c3) + `,"condition_id":"` + c3 + `","question_id":"` + fixedCondHash("q3") + `","tick_size":"0.01","min_size":5}
		]
	}`)
	rec := doRequest(t, mux, http.MethodPost, "/admin/events/binary", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%q, want 201", rec.Code, rec.Body.String())
	}
	if len(pub.configCalls) != 3 {
		t.Errorf("PublishMarketConfig calls = %d, want 3", len(pub.configCalls))
	}
}

// binaryEventBodyWithTokens is binaryEventBody with caller-controlled
// token ids, for exercising the token verification paths.
func binaryEventBodyWithTokens(slug, catID, condHex, questionHex, tokenYes, tokenNo string) []byte {
	return []byte(`{
		"slug":"` + slug + `","title":"T","description":"D",
		"category_id":"` + catID + `",
		"end_date":"` + time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339) + `",
		"markets":[{
			"slug":"` + slug + `-m1","question":"Q?",
			"outcome_yes_label":"Yes","outcome_no_label":"No",
			"token_id_yes":"` + tokenYes + `","token_id_no":"` + tokenNo + `",
			"condition_id":"` + condHex + `","question_id":"` + questionHex + `",
			"tick_size":"0.01","min_size":5
		}]
	}`)
}

func TestHandler_CreateEvent_Binary_TokenIDMismatch(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	chain := newFakeChainReader()
	cond := fixedCondHash("tok-mismatch")
	chain.slotCount[common.HexToHash(cond)] = 2
	mux := muxWithChain(t, repo, &fakePublisher{}, chain)

	// Valid decimal token ids that don't match the fake derivation.
	rec := doRequest(t, mux, http.MethodPost, "/admin/events/binary",
		binaryEventBodyWithTokens("tok-mismatch", catID, cond, fixedCondHash("q"), "12345", "67890"))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d body=%q, want 422", rec.Code, rec.Body.String())
	}
}

func TestHandler_CreateEvent_Binary_TokenIDSwapped(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	chain := newFakeChainReader()
	cond := fixedCondHash("tok-swap")
	chain.slotCount[common.HexToHash(cond)] = 2
	mux := muxWithChain(t, repo, &fakePublisher{}, chain)

	// YES and NO transposed — must be rejected or resolutions pay the
	// wrong side.
	yes, no := fakeTokenPair(common.HexToHash(cond))
	rec := doRequest(t, mux, http.MethodPost, "/admin/events/binary",
		binaryEventBodyWithTokens("tok-swap", catID, cond, fixedCondHash("q"), no.String(), yes.String()))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d body=%q, want 422", rec.Code, rec.Body.String())
	}
}

func TestHandler_CreateEvent_Binary_TokenIDMalformed(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	chain := newFakeChainReader()
	cond := fixedCondHash("tok-bad")
	chain.slotCount[common.HexToHash(cond)] = 2
	mux := muxWithChain(t, repo, &fakePublisher{}, chain)

	for _, tc := range []struct{ name, yes, no string }{
		{"non-decimal", "0xabc", "123"},
		{"empty", "", "123"},
		{"equal", "123", "123"},
	} {
		rec := doRequest(t, mux, http.MethodPost, "/admin/events/binary",
			binaryEventBodyWithTokens("tok-bad-"+tc.name, catID, cond, fixedCondHash("q"), tc.yes, tc.no))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d body=%q, want 400", tc.name, rec.Code, rec.Body.String())
		}
	}
}

func TestHandler_CreateEvent_Binary_TokenIDChainError(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	chain := newFakeChainReader()
	cond := fixedCondHash("tok-err")
	chain.slotCount[common.HexToHash(cond)] = 2
	chain.ctPositionIDErr[common.HexToHash(cond)] = errors.New("rpc down")
	mux := muxWithChain(t, repo, &fakePublisher{}, chain)

	rec := doRequest(t, mux, http.MethodPost, "/admin/events/binary",
		binaryEventBody("tok-err", catID, cond, fixedCondHash("q")))
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d body=%q, want 502", rec.Code, rec.Body.String())
	}
}

// --- /admin/events POST (NEG_RISK) --------------------------------------

func TestHandler_CreateEvent_NegRisk_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "politics")
	pub := &fakePublisher{}
	chain := newFakeChainReader()
	negRiskID := negRiskMarketIDHex("neg-success")
	q1 := negRiskQuestionID(negRiskID, 0)
	q2 := negRiskQuestionID(negRiskID, 1)
	c1 := common.HexToHash(fixedCondHash("dd"))
	c2 := common.HexToHash(fixedCondHash("ee"))
	chain.negRiskCondIDs[common.HexToHash(q1)] = c1
	chain.negRiskCondIDs[common.HexToHash(q2)] = c2
	chain.slotCount[c1] = 2
	chain.slotCount[c2] = 2
	mux := muxWithChain(t, repo, pub, chain)

	body := []byte(`{
		"slug":"neg","title":"T","description":"D",
		"category_id":"` + catID + `",
		"end_date":"` + time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339) + `",
		"neg_risk_market_id":"` + negRiskID + `",
		"markets":[
			{"slug":"alice","question":"Will Alice win?","outcome_yes_label":"Yes","outcome_no_label":"No",` + tokenIDsJSON(q1) + `,"question_id":"` + q1 + `","tick_size":"0.01","min_size":5},
			{"slug":"bob","question":"Will Bob win?","outcome_yes_label":"Yes","outcome_no_label":"No",` + tokenIDsJSON(q2) + `,"question_id":"` + q2 + `","tick_size":"0.01","min_size":5}
		]
	}`)
	rec := doRequest(t, mux, http.MethodPost, "/admin/events/neg-risk", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%q, want 201", rec.Code, rec.Body.String())
	}
	var resp eventWithMarketsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Both derived condition_ids should be persisted into the response.
	for idx, market := range resp.Markets {
		if market.ConditionID == "" {
			t.Errorf("response.markets[%d].condition_id is empty", idx)
		}
	}
}

func TestHandler_CreateEvent_NegRisk_TokenIDMismatch(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	chain := newFakeChainReader()
	negRiskID := negRiskMarketIDHex("neg-tok")
	q1 := negRiskQuestionID(negRiskID, 0)
	q2 := negRiskQuestionID(negRiskID, 1)
	c1 := common.HexToHash(fixedCondHash("nt1"))
	c2 := common.HexToHash(fixedCondHash("nt2"))
	chain.negRiskCondIDs[common.HexToHash(q1)] = c1
	chain.negRiskCondIDs[common.HexToHash(q2)] = c2
	chain.slotCount[c1] = 2
	chain.slotCount[c2] = 2
	mux := muxWithChain(t, repo, &fakePublisher{}, chain)

	// Market b carries decimal token ids that don't match the adapter
	// derivation for q2.
	body := []byte(`{
		"slug":"neg-tok","title":"T","description":"D",
		"category_id":"` + catID + `",
		"end_date":"` + time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339) + `",
		"neg_risk_market_id":"` + negRiskID + `",
		"markets":[
			{"slug":"a","question":"?","outcome_yes_label":"Y","outcome_no_label":"N",` + tokenIDsJSON(q1) + `,"question_id":"` + q1 + `","tick_size":"0.01","min_size":5},
			{"slug":"b","question":"?","outcome_yes_label":"Y","outcome_no_label":"N","token_id_yes":"111","token_id_no":"222","question_id":"` + q2 + `","tick_size":"0.01","min_size":5}
		]
	}`)
	rec := doRequest(t, mux, http.MethodPost, "/admin/events/neg-risk", body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d body=%q, want 422", rec.Code, rec.Body.String())
	}
}

func TestHandler_CreateEvent_NegRisk_MissingMarketID(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	pub := &fakePublisher{}
	chain := newFakeChainReader()
	mux := muxWithChain(t, repo, pub, chain)

	body := []byte(`{
		"slug":"neg-no-id","title":"T","description":"D",
		"category_id":"` + catID + `",
		"end_date":"` + time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339) + `",
		"markets":[
			{"slug":"a","question":"?","outcome_yes_label":"Y","outcome_no_label":"N",` + tokenIDsJSON(fixedCondHash("a")) + `,"question_id":"` + fixedCondHash("a") + `","tick_size":"0.01","min_size":5},
			{"slug":"b","question":"?","outcome_yes_label":"Y","outcome_no_label":"N",` + tokenIDsJSON(fixedCondHash("b")) + `,"question_id":"` + fixedCondHash("b") + `","tick_size":"0.01","min_size":5}
		]
	}`)
	rec := doRequest(t, mux, http.MethodPost, "/admin/events/neg-risk", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandler_CreateEvent_NegRisk_SingleMarket(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	pub := &fakePublisher{}
	chain := newFakeChainReader()
	mux := muxWithChain(t, repo, pub, chain)

	body := []byte(`{
		"slug":"neg-single","title":"T","description":"D",
		"category_id":"` + catID + `",
		"end_date":"` + time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339) + `",
		"neg_risk_market_id":"` + fixedCondHash("m") + `",
		"markets":[
			{"slug":"a","question":"?","outcome_yes_label":"Y","outcome_no_label":"N",` + tokenIDsJSON(fixedCondHash("a")) + `,"question_id":"` + fixedCondHash("a") + `","tick_size":"0.01","min_size":5}
		]
	}`)
	rec := doRequest(t, mux, http.MethodPost, "/admin/events/neg-risk", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// --- /admin/events/{id}/resolve -----------------------------------------

func TestHandler_ResolveEvent_Partial(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	c1 := fixedCondHash("c1")
	c2 := fixedCondHash("c2")
	q1 := fixedCondHash("q1")
	q2 := fixedCondHash("q2")
	pub := &fakePublisher{}
	chain := newFakeChainReader()
	chain.slotCount[common.HexToHash(c1)] = 2
	chain.slotCount[common.HexToHash(c2)] = 2
	mux := muxWithChain(t, repo, pub, chain)

	// Create the event with two markets.
	createBody := []byte(`{
		"slug":"resolve","title":"T","description":"D",
		"category_id":"` + catID + `",
		"end_date":"` + time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339) + `",
		"markets":[
			{"slug":"r-m1","question":"?","outcome_yes_label":"Y","outcome_no_label":"N",` + tokenIDsJSON(c1) + `,"condition_id":"` + c1 + `","question_id":"` + q1 + `","tick_size":"0.01","min_size":5},
			{"slug":"r-m2","question":"?","outcome_yes_label":"Y","outcome_no_label":"N",` + tokenIDsJSON(c2) + `,"condition_id":"` + c2 + `","question_id":"` + q2 + `","tick_size":"0.01","min_size":5}
		]
	}`)
	rec := doRequest(t, mux, http.MethodPost, "/admin/events/binary", createBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: status = %d body=%q", rec.Code, rec.Body.String())
	}
	var created eventWithMarketsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	eventID := created.Event.ID
	m1ID := created.Markets[0].ID
	m2ID := created.Markets[1].ID

	// Pre-load chain: m1's question reported YES = [1,0]. m2 not resolved.
	chain.denominator[common.HexToHash(c1)] = mkBigInt(1)
	chain.numerators[common.HexToHash(c1)] = []*big.Int{mkBigInt(1), mkBigInt(0)}

	// Resolve m1 only.
	pub.configCalls = nil
	pub.statusCalls = nil
	resolveBody := []byte(`{"outcomes":{"` + m1ID + `":"YES"}}`)
	rec = doRequest(t, mux, http.MethodPost, "/admin/events/"+eventID+"/binary/resolve", resolveBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("partial resolve status = %d body=%q", rec.Code, rec.Body.String())
	}
	if len(pub.configCalls) != 1 {
		t.Errorf("config calls = %d, want 1", len(pub.configCalls))
	}
	if len(pub.statusCalls) != 1 {
		t.Errorf("status calls = %d, want 1", len(pub.statusCalls))
	}
	if pub.statusCalls[0].Outcome == nil || *pub.statusCalls[0].Outcome != OutcomeYes {
		t.Errorf("status call outcome = %v, want YES", pub.statusCalls[0].Outcome)
	}

	// Now resolve m2 = NO. Auto-flip should fire.
	chain.denominator[common.HexToHash(c2)] = mkBigInt(1)
	chain.numerators[common.HexToHash(c2)] = []*big.Int{mkBigInt(0), mkBigInt(1)}
	resolveBody = []byte(`{"outcomes":{"` + m2ID + `":"NO"}}`)
	rec = doRequest(t, mux, http.MethodPost, "/admin/events/"+eventID+"/binary/resolve", resolveBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("second resolve status = %d body=%q", rec.Code, rec.Body.String())
	}
	var resp eventWithMarketsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Event.Status != "RESOLVED" {
		t.Errorf("event status after auto-flip = %s, want RESOLVED", resp.Event.Status)
	}
}

func TestHandler_ResolveEvent_OnChainNotResolved(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	c1 := fixedCondHash("c1")
	pub := &fakePublisher{}
	chain := newFakeChainReader()
	chain.slotCount[common.HexToHash(c1)] = 2
	mux := muxWithChain(t, repo, pub, chain)

	createBody := binaryEventBody("notres", catID, c1, fixedCondHash("q1"))
	rec := doRequest(t, mux, http.MethodPost, "/admin/events/binary", createBody)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body.String())
	}
	var created eventWithMarketsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// denominator stays 0 → "payouts not reported".
	resolveBody := []byte(`{"outcomes":{"` + created.Markets[0].ID + `":"YES"}}`)
	rec = doRequest(t, mux, http.MethodPost, "/admin/events/"+created.Event.ID+"/binary/resolve", resolveBody)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
}

func TestHandler_ResolveEvent_OutcomeMismatch(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	c1 := fixedCondHash("c1")
	pub := &fakePublisher{}
	chain := newFakeChainReader()
	chain.slotCount[common.HexToHash(c1)] = 2
	mux := muxWithChain(t, repo, pub, chain)

	rec := doRequest(t, mux, http.MethodPost, "/admin/events/binary",
		binaryEventBody("mismatch", catID, c1, fixedCondHash("q")))
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: %d", rec.Code)
	}
	var created eventWithMarketsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// chain says NO ([0,1]) but admin declares YES.
	chain.denominator[common.HexToHash(c1)] = mkBigInt(1)
	chain.numerators[common.HexToHash(c1)] = []*big.Int{mkBigInt(0), mkBigInt(1)}

	resolveBody := []byte(`{"outcomes":{"` + created.Markets[0].ID + `":"YES"}}`)
	rec = doRequest(t, mux, http.MethodPost, "/admin/events/"+created.Event.ID+"/binary/resolve", resolveBody)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
}

func TestHandler_ResolveEvent_EventNotFound(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	pub := &fakePublisher{}
	chain := newFakeChainReader()
	mux := muxWithChain(t, repo, pub, chain)

	rec := doRequest(t, mux, http.MethodPost, "/admin/events/missing/binary/resolve",
		[]byte(`{"outcomes":{"x":"YES"}}`))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandler_ResolveEvent_NegRiskFriendlyError(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	pub := &fakePublisher{}
	chain := newFakeChainReader()
	negID := negRiskMarketIDHex("neg-fe")
	q1 := negRiskQuestionID(negID, 0)
	q2 := negRiskQuestionID(negID, 1)
	c1 := common.HexToHash(fixedCondHash("cc"))
	c2 := common.HexToHash(fixedCondHash("dd"))
	chain.negRiskCondIDs[common.HexToHash(q1)] = c1
	chain.negRiskCondIDs[common.HexToHash(q2)] = c2
	chain.slotCount[c1] = 2
	chain.slotCount[c2] = 2
	mux := muxWithChain(t, repo, pub, chain)

	body := []byte(`{
		"slug":"neg-fe","title":"T","description":"D",
		"category_id":"` + catID + `",
		"end_date":"` + time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339) + `",
		"neg_risk_market_id":"` + negID + `",
		"markets":[
			{"slug":"a","question":"?","outcome_yes_label":"Y","outcome_no_label":"N",` + tokenIDsJSON(q1) + `,"question_id":"` + q1 + `","tick_size":"0.01","min_size":5},
			{"slug":"b","question":"?","outcome_yes_label":"Y","outcome_no_label":"N",` + tokenIDsJSON(q2) + `,"question_id":"` + q2 + `","tick_size":"0.01","min_size":5}
		]
	}`)
	rec := doRequest(t, mux, http.MethodPost, "/admin/events/neg-risk", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body.String())
	}
	var created eventWithMarketsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// On-chain state: q1 has been resolved YES (denominator>0, numerators=[1,0]).
	// Admin tries to resolve q2 as YES. q2's denominator=0 AND getDetermined=true.
	chain.denominator[c1] = mkBigInt(1)
	chain.numerators[c1] = []*big.Int{mkBigInt(1), mkBigInt(0)}
	// q2 has no payouts (denominator stays 0).
	chain.negRiskDetermined[common.HexToHash(negID)] = true

	resolveBody := []byte(`{"outcomes":{"` + created.Markets[1].ID + `":"YES"}}`)
	rec = doRequest(t, mux, http.MethodPost, "/admin/events/"+created.Event.ID+"/neg-risk/resolve", resolveBody)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	// Friendly NegRisk message should be in the body.
	if !bytes.Contains(rec.Body.Bytes(), []byte("NegRisk")) && !bytes.Contains(rec.Body.Bytes(), []byte("already resolved YES")) {
		t.Errorf("expected friendlier NegRisk error message, got: %q", rec.Body.String())
	}
}

// --- /admin/events/{id}/void --------------------------------------------

func TestHandler_VoidEvent_Binary_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	c1 := fixedCondHash("v1")
	pub := &fakePublisher{}
	chain := newFakeChainReader()
	chain.slotCount[common.HexToHash(c1)] = 2
	mux := muxWithChain(t, repo, pub, chain)

	rec := doRequest(t, mux, http.MethodPost, "/admin/events/binary",
		binaryEventBody("void", catID, c1, fixedCondHash("vq1")))
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body.String())
	}
	var created eventWithMarketsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// Equal positive numerators ([1,1]) indicate void.
	chain.denominator[common.HexToHash(c1)] = mkBigInt(2)
	chain.numerators[common.HexToHash(c1)] = []*big.Int{mkBigInt(1), mkBigInt(1)}

	pub.configCalls = nil
	pub.statusCalls = nil
	rec = doRequest(t, mux, http.MethodPost, "/admin/events/"+created.Event.ID+"/void",
		[]byte(`{"market_ids":["`+created.Markets[0].ID+`"]}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%q", rec.Code, rec.Body.String())
	}
	if len(pub.configCalls) != 1 || len(pub.statusCalls) != 1 {
		t.Errorf("publishes: config=%d status=%d, want 1/1",
			len(pub.configCalls), len(pub.statusCalls))
	}
	if pub.statusCalls[0].Outcome != nil {
		t.Errorf("void status call outcome = %v, want nil", pub.statusCalls[0].Outcome)
	}
}

func TestHandler_VoidEvent_Binary_DefaultAllActive(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	c1 := fixedCondHash("dv1")
	pub := &fakePublisher{}
	chain := newFakeChainReader()
	chain.slotCount[common.HexToHash(c1)] = 2
	mux := muxWithChain(t, repo, pub, chain)

	rec := doRequest(t, mux, http.MethodPost, "/admin/events/binary",
		binaryEventBody("dv", catID, c1, fixedCondHash("dvq1")))
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body.String())
	}
	var created eventWithMarketsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	chain.denominator[common.HexToHash(c1)] = mkBigInt(2)
	chain.numerators[common.HexToHash(c1)] = []*big.Int{mkBigInt(1), mkBigInt(1)}

	// No body / no market_ids → defaults to "all active".
	rec = doRequest(t, mux, http.MethodPost, "/admin/events/"+created.Event.ID+"/void", []byte(`{}`))
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d body=%q, want 200", rec.Code, rec.Body.String())
	}
}

func TestHandler_VoidEvent_Binary_UnequalPayouts(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	c1 := fixedCondHash("uv1")
	pub := &fakePublisher{}
	chain := newFakeChainReader()
	chain.slotCount[common.HexToHash(c1)] = 2
	mux := muxWithChain(t, repo, pub, chain)

	rec := doRequest(t, mux, http.MethodPost, "/admin/events/binary",
		binaryEventBody("uv", catID, c1, fixedCondHash("uvq1")))
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: %d", rec.Code)
	}
	var created eventWithMarketsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	// [1,0] is a resolution, not a void.
	chain.denominator[common.HexToHash(c1)] = mkBigInt(1)
	chain.numerators[common.HexToHash(c1)] = []*big.Int{mkBigInt(1), mkBigInt(0)}

	rec = doRequest(t, mux, http.MethodPost, "/admin/events/"+created.Event.ID+"/void",
		[]byte(`{"market_ids":["`+created.Markets[0].ID+`"]}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", rec.Code)
	}
}

func TestHandler_VoidEvent_NegRisk_AlwaysRejected(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	pub := &fakePublisher{}
	chain := newFakeChainReader()
	negRiskID := negRiskMarketIDHex("neg-void")
	q1 := negRiskQuestionID(negRiskID, 0)
	q2 := negRiskQuestionID(negRiskID, 1)
	c1 := common.HexToHash(fixedCondHash("cc"))
	c2 := common.HexToHash(fixedCondHash("dd"))
	chain.negRiskCondIDs[common.HexToHash(q1)] = c1
	chain.negRiskCondIDs[common.HexToHash(q2)] = c2
	chain.slotCount[c1] = 2
	chain.slotCount[c2] = 2
	mux := muxWithChain(t, repo, pub, chain)

	body := []byte(`{
		"slug":"neg-void","title":"T","description":"D",
		"category_id":"` + catID + `",
		"end_date":"` + time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339) + `",
		"neg_risk_market_id":"` + negRiskID + `",
		"markets":[
			{"slug":"a","question":"?","outcome_yes_label":"Y","outcome_no_label":"N",` + tokenIDsJSON(q1) + `,"question_id":"` + q1 + `","tick_size":"0.01","min_size":5},
			{"slug":"b","question":"?","outcome_yes_label":"Y","outcome_no_label":"N",` + tokenIDsJSON(q2) + `,"question_id":"` + q2 + `","tick_size":"0.01","min_size":5}
		]
	}`)
	rec := doRequest(t, mux, http.MethodPost, "/admin/events/neg-risk", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body.String())
	}
	var created eventWithMarketsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	for _, requestBody := range [][]byte{
		[]byte(`{}`),
		[]byte(`{"market_ids":["` + created.Markets[0].ID + `"]}`),
		nil,
	} {
		rec = doRequest(t, mux, http.MethodPost, "/admin/events/"+created.Event.ID+"/void", requestBody)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body=%q: status = %d, want 400", requestBody, rec.Code)
		}
		if !bytes.Contains(rec.Body.Bytes(), []byte("void_not_supported_for_negrisk")) {
			t.Errorf("body=%q: response missing expected error code, got: %q",
				requestBody, rec.Body.String())
		}
	}
}

// --- /admin/markets/{id}/trading-config ---------------------------------

func TestHandler_SetTradingConfig_Success(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putMarket(&Market{Question: "Q?", Status: StatusActive, TokenIDYes: "y", TokenIDNo: "n"})
	pub := &fakePublisher{}
	mux := muxWithChain(t, repo, pub, newFakeChainReader())

	body := mustJSON(t, tradingConfigRequest{TickSize: "0.001", MinSize: 10})
	rec := doRequest(t, mux, http.MethodPut, "/admin/markets/"+seed.ID+"/trading-config", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%q", rec.Code, rec.Body.String())
	}
	if len(pub.configCalls) != 1 {
		t.Errorf("PublishMarketConfig calls = %d, want 1", len(pub.configCalls))
	}
}

func TestHandler_SetTradingConfig_InvalidTickSize(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putMarket(&Market{Question: "Q?", Status: StatusActive, TokenIDYes: "y", TokenIDNo: "n"})
	mux := muxWithChain(t, repo, &fakePublisher{}, newFakeChainReader())

	body := []byte(`{"tick_size":"0.5","min_size":5}`)
	rec := doRequest(t, mux, http.MethodPut, "/admin/markets/"+seed.ID+"/trading-config", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandler_SetTradingConfig_NotFound(t *testing.T) {
	t.Parallel()
	mux := muxWithChain(t, newFakeRepo(), &fakePublisher{}, newFakeChainReader())

	body := mustJSON(t, tradingConfigRequest{TickSize: "0.01", MinSize: 5})
	rec := doRequest(t, mux, http.MethodPut, "/admin/markets/missing/trading-config", body)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestHandler_SetTradingConfig_PublishFailure(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putMarket(&Market{Question: "Q?", Status: StatusActive, TokenIDYes: "y", TokenIDNo: "n"})
	pub := &fakePublisher{configErr: errors.New("KV down")}
	mux := muxWithChain(t, repo, pub, newFakeChainReader())

	body := mustJSON(t, tradingConfigRequest{TickSize: "0.001", MinSize: 10})
	rec := doRequest(t, mux, http.MethodPut, "/admin/markets/"+seed.ID+"/trading-config", body)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

// --- live-status overlay on GET /admin/markets/{id} ----------------------

func TestHandler_GetMarket_StatusComesFromBucket(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	// DB says RESOLVED, but the KV publish never landed — the bucket
	// still carries ACTIVE. The admin must see ACTIVE.
	outcome := OutcomeYes
	seed := repo.putMarket(&Market{
		Question: "Q?", Status: StatusResolved, Outcome: &outcome,
		TokenIDYes: "y", TokenIDNo: "n",
	})
	pub := &fakePublisher{liveStatus: map[string]string{seed.ID: "ACTIVE"}}
	mux := muxWithPublisher(t, repo, pub)

	rec := doRequest(t, mux, http.MethodGet, "/admin/markets/"+seed.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", rec.Code, rec.Body.String())
	}
	var got marketResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != "ACTIVE" {
		t.Errorf("status = %q, want ACTIVE (bucket state, not DB state)", got.Status)
	}
}

func TestHandler_GetMarket_NoBucketEntryShowsUnpublished(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putMarket(&Market{Question: "Q?", Status: StatusActive, TokenIDYes: "y", TokenIDNo: "n"})
	pub := &fakePublisher{} // no liveStatus entries
	mux := muxWithPublisher(t, repo, pub)

	rec := doRequest(t, mux, http.MethodGet, "/admin/markets/"+seed.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got marketResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != liveStatusUnpublished {
		t.Errorf("status = %q, want %q", got.Status, liveStatusUnpublished)
	}
}

func TestHandler_GetMarket_BucketReadFailureReturns502(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	seed := repo.putMarket(&Market{Question: "Q?", Status: StatusActive, TokenIDYes: "y", TokenIDNo: "n"})
	pub := &fakePublisher{liveStatusErr: errors.New("KV down")}
	mux := muxWithPublisher(t, repo, pub)

	rec := doRequest(t, mux, http.MethodGet, "/admin/markets/"+seed.ID, nil)
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

// --- idempotent retry after a failed publish ------------------------------

func TestHandler_ResolveEvent_RetrySameOutcomeRepublishes(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	c1 := fixedCondHash("ri1")
	pub := &fakePublisher{}
	chain := newFakeChainReader()
	chain.slotCount[common.HexToHash(c1)] = 2
	mux := muxWithChain(t, repo, pub, chain)

	rec := doRequest(t, mux, http.MethodPost, "/admin/events/binary",
		binaryEventBody("retry-resolve", catID, c1, fixedCondHash("riq1")))
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body.String())
	}
	var created eventWithMarketsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	marketID := created.Markets[0].ID

	chain.denominator[common.HexToHash(c1)] = mkBigInt(1)
	chain.numerators[common.HexToHash(c1)] = []*big.Int{mkBigInt(1), mkBigInt(0)}

	// First resolve: DB commits, but the publish fails — 502.
	pub.configErr = errors.New("KV down")
	resolveBody := []byte(`{"outcomes":{"` + marketID + `":"YES"}}`)
	rec = doRequest(t, mux, http.MethodPost, "/admin/events/"+created.Event.ID+"/binary/resolve", resolveBody)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("first resolve status = %d, want 502", rec.Code)
	}
	if repo.markets[marketID].Status != StatusResolved {
		t.Fatalf("market status = %s, want RESOLVED (DB committed before publish)",
			repo.markets[marketID].Status)
	}

	// Retry with the same outcome: idempotent no-op in the DB, and the
	// publishes run again — this is the recovery path for stale KV.
	pub.configErr = nil
	pub.configCalls = nil
	pub.statusCalls = nil
	rec = doRequest(t, mux, http.MethodPost, "/admin/events/"+created.Event.ID+"/binary/resolve", resolveBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("retry status = %d body=%q, want 200", rec.Code, rec.Body.String())
	}
	if len(pub.configCalls) != 1 {
		t.Errorf("config publishes on retry = %d, want 1", len(pub.configCalls))
	}
	if len(pub.statusCalls) != 1 {
		t.Errorf("status publishes on retry = %d, want 1", len(pub.statusCalls))
	}
}

func TestHandler_ResolveEvent_RetryDifferentOutcomeConflicts(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	c1 := fixedCondHash("rd1")
	pub := &fakePublisher{}
	chain := newFakeChainReader()
	chain.slotCount[common.HexToHash(c1)] = 2
	mux := muxWithChain(t, repo, pub, chain)

	rec := doRequest(t, mux, http.MethodPost, "/admin/events/binary",
		binaryEventBody("retry-diff", catID, c1, fixedCondHash("rdq1")))
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body.String())
	}
	var created eventWithMarketsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	marketID := created.Markets[0].ID

	chain.denominator[common.HexToHash(c1)] = mkBigInt(1)
	chain.numerators[common.HexToHash(c1)] = []*big.Int{mkBigInt(1), mkBigInt(0)}
	rec = doRequest(t, mux, http.MethodPost, "/admin/events/"+created.Event.ID+"/binary/resolve",
		[]byte(`{"outcomes":{"`+marketID+`":"YES"}}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("first resolve: %d %s", rec.Code, rec.Body.String())
	}

	// Declaring NO now contradicts both the stored outcome and the chain
	// — chain verification rejects it first with a 422.
	rec = doRequest(t, mux, http.MethodPost, "/admin/events/"+created.Event.ID+"/binary/resolve",
		[]byte(`{"outcomes":{"`+marketID+`":"NO"}}`))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("conflicting retry status = %d, want 422", rec.Code)
	}
}

func TestHandler_VoidEvent_RetryRepublishes(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	c1 := fixedCondHash("rv1")
	pub := &fakePublisher{}
	chain := newFakeChainReader()
	chain.slotCount[common.HexToHash(c1)] = 2
	mux := muxWithChain(t, repo, pub, chain)

	rec := doRequest(t, mux, http.MethodPost, "/admin/events/binary",
		binaryEventBody("retry-void", catID, c1, fixedCondHash("rvq1")))
	if rec.Code != http.StatusCreated {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body.String())
	}
	var created eventWithMarketsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	marketID := created.Markets[0].ID

	chain.denominator[common.HexToHash(c1)] = mkBigInt(2)
	chain.numerators[common.HexToHash(c1)] = []*big.Int{mkBigInt(1), mkBigInt(1)}

	voidBody := []byte(`{"market_ids":["` + marketID + `"]}`)
	pub.configErr = errors.New("KV down")
	rec = doRequest(t, mux, http.MethodPost, "/admin/events/"+created.Event.ID+"/void", voidBody)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("first void status = %d, want 502", rec.Code)
	}

	pub.configErr = nil
	pub.configCalls = nil
	pub.statusCalls = nil
	rec = doRequest(t, mux, http.MethodPost, "/admin/events/"+created.Event.ID+"/void", voidBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("retry status = %d body=%q, want 200", rec.Code, rec.Body.String())
	}
	if len(pub.configCalls) != 1 || len(pub.statusCalls) != 1 {
		t.Errorf("publishes on retry: config=%d status=%d, want 1/1",
			len(pub.configCalls), len(pub.statusCalls))
	}
}

// --- create-time ID validation --------------------------------------------

func TestHandler_CreateEvent_Binary_MalformedConditionID(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	mux := muxWithChain(t, repo, &fakePublisher{}, newFakeChainReader())

	rec := doRequest(t, mux, http.MethodPost, "/admin/events/binary",
		binaryEventBody("badcond", catID, "0xnot-valid-hex", fixedCondHash("q")))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("condition_id")) {
		t.Errorf("error should name condition_id, got: %q", rec.Body.String())
	}
}

func TestHandler_CreateEvent_Binary_MalformedQuestionID(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	mux := muxWithChain(t, repo, &fakePublisher{}, newFakeChainReader())

	rec := doRequest(t, mux, http.MethodPost, "/admin/events/binary",
		binaryEventBody("badq", catID, fixedCondHash("c"), "0x1234"))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("question_id")) {
		t.Errorf("error should name question_id, got: %q", rec.Body.String())
	}
}

func TestHandler_CreateEvent_NegRisk_QuestionIDFromDifferentMarket(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	mux := muxWithChain(t, repo, &fakePublisher{}, newFakeChainReader())

	negRiskID := negRiskMarketIDHex("market-one")
	foreignID := negRiskMarketIDHex("market-two")
	body := []byte(`{
		"slug":"neg-mismatch","title":"T","description":"D",
		"category_id":"` + catID + `",
		"end_date":"` + time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339) + `",
		"neg_risk_market_id":"` + negRiskID + `",
		"markets":[
			{"slug":"a","question":"?","outcome_yes_label":"Y","outcome_no_label":"N",` + tokenIDsJSON(negRiskQuestionID(negRiskID, 0)) + `,"question_id":"` + negRiskQuestionID(negRiskID, 0) + `","tick_size":"0.01","min_size":5},
			{"slug":"b","question":"?","outcome_yes_label":"Y","outcome_no_label":"N",` + tokenIDsJSON(negRiskQuestionID(foreignID, 1)) + `,"question_id":"` + negRiskQuestionID(foreignID, 1) + `","tick_size":"0.01","min_size":5}
		]
	}`)
	rec := doRequest(t, mux, http.MethodPost, "/admin/events/neg-risk", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("does not belong to neg_risk_market_id")) {
		t.Errorf("error should explain the mismatch, got: %q", rec.Body.String())
	}
}

func TestHandler_CreateEvent_NegRisk_MarketIDWithNonZeroFinalByte(t *testing.T) {
	t.Parallel()
	repo := newFakeRepo()
	catID := seedCatID(t, repo, "x")
	mux := muxWithChain(t, repo, &fakePublisher{}, newFakeChainReader())

	// A questionId pasted where the marketId belongs: final byte non-zero.
	questionIDAsMarketID := negRiskQuestionID(negRiskMarketIDHex("market-one"), 1)
	body := []byte(`{
		"slug":"neg-badmid","title":"T","description":"D",
		"category_id":"` + catID + `",
		"end_date":"` + time.Now().Add(24*time.Hour).UTC().Format(time.RFC3339) + `",
		"neg_risk_market_id":"` + questionIDAsMarketID + `",
		"markets":[
			{"slug":"a","question":"?","outcome_yes_label":"Y","outcome_no_label":"N",` + tokenIDsJSON(negRiskQuestionID(questionIDAsMarketID, 0)) + `,"question_id":"` + negRiskQuestionID(questionIDAsMarketID, 0) + `","tick_size":"0.01","min_size":5},
			{"slug":"b","question":"?","outcome_yes_label":"Y","outcome_no_label":"N",` + tokenIDsJSON(negRiskQuestionID(questionIDAsMarketID, 1)) + `,"question_id":"` + negRiskQuestionID(questionIDAsMarketID, 1) + `","tick_size":"0.01","min_size":5}
		]
	}`)
	rec := doRequest(t, mux, http.MethodPost, "/admin/events/neg-risk", body)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("final byte must be zero")) {
		t.Errorf("error should explain the marketId shape, got: %q", rec.Body.String())
	}
}
