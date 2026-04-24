package market

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

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
	if err := ValidateStatusTransition(m.Status, status); err != nil {
		return nil, err
	}
	m.Status = status
	return m, nil
}

func (f *fakeRepo) UpsertFeeRate(_ context.Context, rate *FeeRate) (*FeeRate, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	if err := ValidateFeeRate(rate); err != nil {
		return nil, err
	}
	if _, ok := f.markets[rate.MarketID]; !ok {
		return nil, ErrNotFound
	}
	stored := &FeeRate{
		MarketID:   rate.MarketID,
		FeeRateBps: rate.FeeRateBps,
		UpdatedAt:  time.Now().UTC(),
	}
	out := *stored
	return &out, nil
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

func newHandlerForTest(t *testing.T, repo Repository) *Handler {
	t.Helper()
	logger := zap.NewNop()
	return NewHandler(repo, logger)
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
