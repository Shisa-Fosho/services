package market

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.uber.org/zap"
)

// fakeRepo is an in-memory Repository double. Only the category methods are
// implemented meaningfully — the rest return nil/zero to satisfy the
// interface. Tests can set hooks to force errors.
type fakeRepo struct {
	Repository // embed to get default-nil implementations of unused methods
	byID       map[string]*Category
	bySlug     map[string]*Category
	nextID     int

	createErr error
	updateErr error
	deleteErr error
	getErr    error
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		byID:   map[string]*Category{},
		bySlug: map[string]*Category{},
	}
}

func (f *fakeRepo) CreateCategory(_ context.Context, cat *Category) error {
	if f.createErr != nil {
		return f.createErr
	}
	if _, exists := f.bySlug[cat.Slug]; exists {
		return ErrDuplicateSlug
	}
	f.nextID++
	cat.ID = "cat-" + itoa(f.nextID)
	stored := &Category{ID: cat.ID, Name: cat.Name, Slug: cat.Slug}
	f.byID[cat.ID] = stored
	f.bySlug[cat.Slug] = stored
	return nil
}

func (f *fakeRepo) GetCategory(_ context.Context, id string) (*Category, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	cat, ok := f.byID[id]
	if !ok {
		return nil, ErrNotFound
	}
	return cat, nil
}

func (f *fakeRepo) UpdateCategory(_ context.Context, id, name, slug string) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	cat, ok := f.byID[id]
	if !ok {
		return ErrNotFound
	}
	if other, exists := f.bySlug[slug]; exists && other.ID != id {
		return ErrDuplicateSlug
	}
	delete(f.bySlug, cat.Slug)
	cat.Name = name
	cat.Slug = slug
	f.bySlug[slug] = cat
	return nil
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

// itoa is a tiny int→string helper to avoid pulling in strconv in test setup.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 4)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
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
