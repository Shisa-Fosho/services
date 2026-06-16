//go:build integration

package market

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Shisa-Fosho/services/internal/shared/postgres"
)

func cleanTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx,
		`TRUNCATE markets, events, categories CASCADE`)
	if err != nil {
		t.Fatalf("cleaning tables: %v", err)
	}
}

// seedCategory creates a category with a unique slug and returns its ID.
func seedCategory(t *testing.T, repo *PGRepository, slug string) string {
	t.Helper()
	cat := &Category{Name: slug, Slug: slug}
	if err := repo.CreateCategory(context.Background(), cat); err != nil {
		t.Fatalf("seeding category %q: %v", slug, err)
	}
	return cat.ID
}

// seedBinaryEvent creates a category + a BINARY event with one market via
// CreateEventWithMarket, and returns the event id and market id.
func seedBinaryEvent(t *testing.T, repo *PGRepository, slug string) (string, string) {
	t.Helper()
	catID := seedCategory(t, repo, slug+"-cat")
	event := &Event{
		Slug:             slug + "-event",
		Title:            slug,
		CategoryID:       catID,
		EventType:        EventTypeBinary,
		ResolutionConfig: json.RawMessage(`{}`),
		Status:           StatusActive,
		EndDate:          time.Now().Add(30 * 24 * time.Hour),
	}
	market := defaultMarket(slug)
	createdEvent, createdMarket, err := repo.CreateEventWithMarket(context.Background(), event, market)
	if err != nil {
		t.Fatalf("seeding event+market: %v", err)
	}
	return createdEvent.ID, createdMarket.ID
}

// seedBinaryEventWithMarkets creates a BINARY event with the first market at
// creation, then appends the rest one at a time (mirroring the production
// one-at-a-time model). Returns the event id and the ordered market ids.
// Used to set up multi-market events for resolve/void tests.
func seedBinaryEventWithMarkets(t *testing.T, repo *PGRepository, slug string, marketSlugs ...string) (string, []string) {
	t.Helper()
	if len(marketSlugs) == 0 {
		t.Fatalf("seedBinaryEventWithMarkets: need at least one market slug")
	}
	catID := seedCategory(t, repo, slug+"-cat")
	event := &Event{
		Slug:             slug + "-event",
		Title:            slug,
		CategoryID:       catID,
		EventType:        EventTypeBinary,
		ResolutionConfig: json.RawMessage(`{}`),
		Status:           StatusActive,
		EndDate:          time.Now().Add(30 * 24 * time.Hour),
	}
	createdEvent, firstMarket, err := repo.CreateEventWithMarket(context.Background(), event, defaultMarket(marketSlugs[0]))
	if err != nil {
		t.Fatalf("seeding event+market: %v", err)
	}
	ids := []string{firstMarket.ID}
	for _, marketSlug := range marketSlugs[1:] {
		_, added, err := repo.AddMarketToEvent(context.Background(), createdEvent.ID, defaultMarket(marketSlug))
		if err != nil {
			t.Fatalf("appending market %q: %v", marketSlug, err)
		}
		ids = append(ids, added.ID)
	}
	return createdEvent.ID, ids
}

// defaultMarket builds a domain Market with all required fields populated
// from a slug, ready for CreateEventWithMarket.
func defaultMarket(slug string) *Market {
	return &Market{
		Slug:            slug,
		Question:        "Question for " + slug + "?",
		OutcomeYesLabel: "Yes",
		OutcomeNoLabel:  "No",
		TokenIDYes:      "ty-" + slug,
		TokenIDNo:       "tn-" + slug,
		ConditionID:     "c-" + slug,
		QuestionID:      "q-" + slug,
		Status:          StatusActive,
		TickSize:        TickSize0_01,
		MinSize:         5,
		PriceYes:        50,
		PriceNo:         50,
	}
}

// --- categories -----------------------------------------------------------

func TestPGRepository_CreateAndGetCategory(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	cat := &Category{Name: "Sports", Slug: "sports"}
	if err := repo.CreateCategory(ctx, cat); err != nil {
		t.Fatalf("creating category: %v", err)
	}

	cats, err := repo.ListCategories(ctx)
	if err != nil {
		t.Fatalf("listing categories: %v", err)
	}
	if len(cats) == 0 {
		t.Fatal("expected at least one category")
	}

	got, err := repo.GetCategory(ctx, cats[0].ID)
	if err != nil {
		t.Fatalf("getting category: %v", err)
	}
	if got.Name != "Sports" {
		t.Errorf("category name = %q, want %q", got.Name, "Sports")
	}
	if got.Slug != "sports" {
		t.Errorf("category slug = %q, want %q", got.Slug, "sports")
	}
}

func TestPGRepository_CreateCategory_DuplicateSlug(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	cat := &Category{Name: "Sports", Slug: "sports"}
	if err := repo.CreateCategory(ctx, cat); err != nil {
		t.Fatalf("creating category: %v", err)
	}

	err := repo.CreateCategory(ctx, &Category{Name: "Sports 2", Slug: "sports"})
	if !errors.Is(err, ErrDuplicateSlug) {
		t.Errorf("expected ErrDuplicateSlug, got: %v", err)
	}
}

func TestPGRepository_GetCategory_NotFound(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, err := repo.GetCategory(ctx, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestPGRepository_UpdateCategory(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	if err := repo.CreateCategory(ctx, &Category{Name: "Sports", Slug: "sports"}); err != nil {
		t.Fatalf("creating category: %v", err)
	}
	cats, _ := repo.ListCategories(ctx)
	id := cats[0].ID

	got, err := repo.UpdateCategory(ctx, id, "Sports & Entertainment", "sports-ent")
	if err != nil {
		t.Fatalf("updating category: %v", err)
	}
	if got.ID != id {
		t.Errorf("id = %q, want %q", got.ID, id)
	}
	if got.Name != "Sports & Entertainment" {
		t.Errorf("name = %q, want %q", got.Name, "Sports & Entertainment")
	}
	if got.Slug != "sports-ent" {
		t.Errorf("slug = %q, want %q", got.Slug, "sports-ent")
	}
}

func TestPGRepository_UpdateCategory_NotFound(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, err := repo.UpdateCategory(ctx,
		"00000000-0000-0000-0000-000000000000", "Ghost", "ghost")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestPGRepository_UpdateCategory_DuplicateSlug(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	if err := repo.CreateCategory(ctx, &Category{Name: "Sports", Slug: "sports"}); err != nil {
		t.Fatalf("creating first category: %v", err)
	}
	if err := repo.CreateCategory(ctx, &Category{Name: "Politics", Slug: "politics"}); err != nil {
		t.Fatalf("creating second category: %v", err)
	}
	cats, _ := repo.ListCategories(ctx)
	var politicsID string
	for _, c := range cats {
		if c.Slug == "politics" {
			politicsID = c.ID
		}
	}

	_, err := repo.UpdateCategory(ctx, politicsID, "Politics", "sports")
	if !errors.Is(err, ErrDuplicateSlug) {
		t.Errorf("expected ErrDuplicateSlug, got: %v", err)
	}
}

func TestPGRepository_DeleteCategory(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	if err := repo.CreateCategory(ctx, &Category{Name: "Sports", Slug: "sports"}); err != nil {
		t.Fatalf("creating category: %v", err)
	}
	cats, _ := repo.ListCategories(ctx)
	id := cats[0].ID

	if err := repo.DeleteCategory(ctx, id); err != nil {
		t.Fatalf("deleting category: %v", err)
	}

	_, err := repo.GetCategory(ctx, id)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound after delete, got: %v", err)
	}
}

func TestPGRepository_DeleteCategory_NotFound(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	err := repo.DeleteCategory(ctx, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

// --- events + markets create -------------------------------------------------

func TestPGRepository_CreateEventWithMarket_Binary(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	catID := seedCategory(t, repo, "politics")
	event := &Event{
		Slug:             "us-election-2024",
		Title:            "2024 US Presidential Election",
		Description:      "Who will win?",
		CategoryID:       catID,
		EventType:        EventTypeBinary,
		ResolutionConfig: json.RawMessage(`{}`),
		Status:           StatusActive,
		EndDate:          time.Now().Add(30 * 24 * time.Hour),
	}
	market := defaultMarket("election-trump-wins")

	createdEvent, createdMarket, err := repo.CreateEventWithMarket(ctx, event, market)
	if err != nil {
		t.Fatalf("creating event with market: %v", err)
	}
	if createdEvent.ID == "" {
		t.Error("expected non-empty event id")
	}
	if createdMarket.EventID != createdEvent.ID {
		t.Errorf("market.event_id = %q, want %q", createdMarket.EventID, createdEvent.ID)
	}
	if createdMarket.QuestionID != market.QuestionID {
		t.Errorf("market.question_id = %q, want %q", createdMarket.QuestionID, market.QuestionID)
	}
}

func TestPGRepository_CreateEventWithMarket_NegRisk(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	catID := seedCategory(t, repo, "politics")
	negRiskMarketID := "0xneg-risk-1"
	event := &Event{
		Slug:             "election-candidates",
		Title:            "Election Candidates",
		CategoryID:       catID,
		EventType:        EventTypeNegRisk,
		ResolutionConfig: json.RawMessage(`{}`),
		Status:           StatusActive,
		EndDate:          time.Now().Add(30 * 24 * time.Hour),
		NegRiskMarketID:  &negRiskMarketID,
	}
	market := defaultMarket("alice")

	createdEvent, createdMarket, err := repo.CreateEventWithMarket(ctx, event, market)
	if err != nil {
		t.Fatalf("creating neg-risk event: %v", err)
	}
	if createdEvent.NegRiskMarketID == nil || *createdEvent.NegRiskMarketID != negRiskMarketID {
		t.Errorf("event.neg_risk_market_id = %v, want %q", createdEvent.NegRiskMarketID, negRiskMarketID)
	}
	if createdMarket.EventID != createdEvent.ID {
		t.Errorf("market.event_id = %q, want %q", createdMarket.EventID, createdEvent.ID)
	}
}

func TestPGRepository_CreateEventWithMarket_RejectsNilMarket(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	catID := seedCategory(t, repo, "x")
	event := &Event{
		Slug:             "no-market",
		Title:            "No Market",
		CategoryID:       catID,
		EventType:        EventTypeBinary,
		ResolutionConfig: json.RawMessage(`{}`),
		Status:           StatusActive,
		EndDate:          time.Now().Add(24 * time.Hour),
	}
	_, _, err := repo.CreateEventWithMarket(ctx, event, nil)
	if !errors.Is(err, ErrInvalidEvent) {
		t.Errorf("expected ErrInvalidEvent, got: %v", err)
	}
}

func TestPGRepository_CreateEventWithMarket_Atomic(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	// Pre-seed an event whose market owns condition_id "c-atomic-seed", then
	// attempt to create a second event whose market collides on that
	// condition_id. The market insert must fail and roll back the
	// just-inserted event row.
	seedBinaryEvent(t, repo, "atomic-seed")

	catID := seedCategory(t, repo, "atomic")
	event := &Event{
		Slug:             "atomic-event",
		Title:            "Atomic",
		CategoryID:       catID,
		EventType:        EventTypeBinary,
		ResolutionConfig: json.RawMessage(`{}`),
		Status:           StatusActive,
		EndDate:          time.Now().Add(24 * time.Hour),
	}
	colliding := defaultMarket("atomic-m1")
	colliding.ConditionID = defaultMarket("atomic-seed").ConditionID // duplicate condition_id

	_, _, err := repo.CreateEventWithMarket(ctx, event, colliding)
	if !errors.Is(err, ErrDuplicateSlug) {
		t.Errorf("expected ErrDuplicateSlug, got: %v", err)
	}

	// Atomicity check: event row should not exist.
	_, err = repo.GetEventBySlug(ctx, "atomic-event")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected event to be rolled back, got: %v", err)
	}
}

func TestPGRepository_CreateEventWithMarket_DuplicateEventSlug(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	seedBinaryEvent(t, repo, "dup")

	// Try to create another event with the same slug.
	catID := seedCategory(t, repo, "second")
	event := &Event{
		Slug:             "dup-event",
		Title:            "Duplicate",
		CategoryID:       catID,
		EventType:        EventTypeBinary,
		ResolutionConfig: json.RawMessage(`{}`),
		Status:           StatusActive,
		EndDate:          time.Now().Add(24 * time.Hour),
	}
	_, _, err := repo.CreateEventWithMarket(ctx, event, defaultMarket("other-market"))
	if !errors.Is(err, ErrDuplicateSlug) {
		t.Errorf("expected ErrDuplicateSlug on event slug, got: %v", err)
	}
}

func TestPGRepository_AddMarketToEvent_Binary(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	eventID, _ := seedBinaryEvent(t, repo, "append-binary")
	market := defaultMarket("append-binary-new")

	event, addedMarket, err := repo.AddMarketToEvent(ctx, eventID, market)
	if err != nil {
		t.Fatalf("adding market: %v", err)
	}
	if event.ID != eventID {
		t.Errorf("event id = %q, want %q", event.ID, eventID)
	}
	if addedMarket.EventID != eventID {
		t.Errorf("market.event_id = %q, want %q", addedMarket.EventID, eventID)
	}

	all, err := repo.ListMarketsByEvent(ctx, eventID)
	if err != nil {
		t.Fatalf("listing markets: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("event markets after append = %d, want 2", len(all))
	}
}

func TestPGRepository_AddMarketToEvent_TerminalEventRejected(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	eventID, _ := seedBinaryEvent(t, repo, "append-terminal")
	if _, err := pool.Exec(ctx, `UPDATE events SET status = $1 WHERE id = $2`, StatusResolved, eventID); err != nil {
		t.Fatalf("marking event resolved: %v", err)
	}

	_, _, err := repo.AddMarketToEvent(ctx, eventID, defaultMarket("append-terminal-new"))
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got: %v", err)
	}
	all, err := repo.ListMarketsByEvent(ctx, eventID)
	if err != nil {
		t.Fatalf("listing markets: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("event markets after rejected append = %d, want 1", len(all))
	}
}

func TestPGRepository_AddMarketToEvent_IdempotentExistingMarket(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	eventID, _ := seedBinaryEvent(t, repo, "append-idem")
	market := defaultMarket("append-idem-new")
	_, addedMarket, err := repo.AddMarketToEvent(ctx, eventID, market)
	if err != nil {
		t.Fatalf("first add: %v", err)
	}

	_, retried, err := repo.AddMarketToEvent(ctx, eventID, defaultMarket("append-idem-new"))
	if err != nil {
		t.Fatalf("retry add: %v", err)
	}
	if retried.ID != addedMarket.ID {
		t.Fatalf("retried market id = %s, want existing %s", retried.ID, addedMarket.ID)
	}
	all, err := repo.ListMarketsByEvent(ctx, eventID)
	if err != nil {
		t.Fatalf("listing markets: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("event markets after idempotent retry = %d, want 2", len(all))
	}
}

func TestPGRepository_GetEvent_NotFound(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, err := repo.GetEvent(ctx, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestPGRepository_GetEventBySlug(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	seedBinaryEvent(t, repo, "slug-lookup")

	got, err := repo.GetEventBySlug(ctx, "slug-lookup-event")
	if err != nil {
		t.Fatalf("getting event by slug: %v", err)
	}
	if got.Slug != "slug-lookup-event" {
		t.Errorf("slug = %q, want slug-lookup-event", got.Slug)
	}
}

func TestPGRepository_ListEvents_StatusFilter(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	seedBinaryEvent(t, repo, "active")
	seedBinaryEvent(t, repo, "paused")
	pausedEventID, _ := seedBinaryEvent(t, repo, "paused-status")
	if _, err := pool.Exec(ctx,
		`UPDATE events SET status = $1 WHERE id = $2`, StatusPaused, pausedEventID,
	); err != nil {
		t.Fatalf("setting event paused: %v", err)
	}

	active, err := repo.ListEvents(ctx, []Status{StatusActive})
	if err != nil {
		t.Fatalf("listing active: %v", err)
	}
	if len(active) != 2 {
		t.Errorf("expected 2 active events, got %d", len(active))
	}

	all, err := repo.ListEvents(ctx, nil)
	if err != nil {
		t.Fatalf("listing all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 events total, got %d", len(all))
	}
}

// --- update paths --------------------------------------------------------

func TestPGRepository_UpdateEvent(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	eventID, _ := seedBinaryEvent(t, repo, "updatable")
	newCat := seedCategory(t, repo, "new-cat")

	newTitle := "Updated Title"
	featured := true
	got, err := repo.UpdateEvent(ctx, eventID, &EventUpdate{
		Title:      &newTitle,
		CategoryID: &newCat,
		Featured:   &featured,
	})
	if err != nil {
		t.Fatalf("updating event: %v", err)
	}
	if got.Title != newTitle {
		t.Errorf("title = %q, want %q", got.Title, newTitle)
	}
	if got.CategoryID != newCat {
		t.Errorf("category_id = %q, want %q", got.CategoryID, newCat)
	}
	if !got.Featured {
		t.Error("featured = false, want true")
	}
}

func TestPGRepository_UpdateEvent_NotFound(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	title := "x"
	_, err := repo.UpdateEvent(ctx, "00000000-0000-0000-0000-000000000000", &EventUpdate{Title: &title})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestPGRepository_GetMarket(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, marketID := seedBinaryEvent(t, repo, "get-market")
	got, err := repo.GetMarket(ctx, marketID)
	if err != nil {
		t.Fatalf("getting market: %v", err)
	}
	if got.Slug != "get-market" {
		t.Errorf("slug = %q", got.Slug)
	}
}

func TestPGRepository_GetMarketBySlug(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	seedBinaryEvent(t, repo, "slug-market")
	got, err := repo.GetMarketBySlug(ctx, "slug-market")
	if err != nil {
		t.Fatalf("getting market by slug: %v", err)
	}
	if got.Slug != "slug-market" {
		t.Errorf("slug = %q", got.Slug)
	}
}

func TestPGRepository_ListMarketsByEvent(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	eventID, _ := seedBinaryEvent(t, repo, "list-by-event")
	markets, err := repo.ListMarketsByEvent(ctx, eventID)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	if len(markets) != 1 {
		t.Errorf("expected 1 market, got %d", len(markets))
	}
}

func TestPGRepository_UpdateStatus(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, marketID := seedBinaryEvent(t, repo, "status-test")

	updated, err := repo.UpdateStatus(ctx, marketID, StatusPaused)
	if err != nil {
		t.Fatalf("updating status to paused: %v", err)
	}
	if updated.Status != StatusPaused {
		t.Errorf("status = %s, want PAUSED", updated.Status)
	}
}

func TestPGRepository_UpdateStatus_InvalidTransition(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, marketID := seedBinaryEvent(t, repo, "bad-trans")
	if _, err := repo.UpdateStatus(ctx, marketID, StatusResolved); err != nil {
		t.Fatalf("resolving market: %v", err)
	}

	_, err := repo.UpdateStatus(ctx, marketID, StatusActive)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got: %v", err)
	}
}

func TestPGRepository_PauseMarkets_Success(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, m1 := seedBinaryEvent(t, repo, "bp-1")
	_, m2 := seedBinaryEvent(t, repo, "bp-2")
	_, m3 := seedBinaryEvent(t, repo, "bp-3")

	updated, err := repo.PauseMarkets(ctx, []string{m3, m1, m2})
	if err != nil {
		t.Fatalf("pausing markets: %v", err)
	}
	if len(updated) != 3 {
		t.Fatalf("updated len = %d, want 3", len(updated))
	}
	// Returned in sorted-ID order, regardless of input order.
	for idx := 1; idx < len(updated); idx++ {
		if updated[idx-1].ID > updated[idx].ID {
			t.Errorf("result not sorted: %s > %s at idx %d",
				updated[idx-1].ID, updated[idx].ID, idx)
		}
	}
	for _, market := range updated {
		if market.Status != StatusPaused {
			t.Errorf("market %s status = %s, want PAUSED", market.ID, market.Status)
		}
	}
}

func TestPGRepository_PauseMarkets_OneNotFound_RollsBack(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, m1 := seedBinaryEvent(t, repo, "bp-rb-1")

	_, err := repo.PauseMarkets(ctx, []string{m1, "00000000-0000-0000-0000-000000000000"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got: %v", err)
	}
	// m1 must remain Active — all-or-nothing.
	got, err := repo.GetMarket(ctx, m1)
	if err != nil {
		t.Fatalf("loading m1: %v", err)
	}
	if got.Status != StatusActive {
		t.Errorf("m1 status = %s, want ACTIVE (transaction must have rolled back)",
			got.Status)
	}
}

func TestPGRepository_PauseMarkets_OneAlreadyPaused_IsIdempotent(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, m1 := seedBinaryEvent(t, repo, "bp-already-1")
	_, m2 := seedBinaryEvent(t, repo, "bp-already-2")
	if _, err := repo.UpdateStatus(ctx, m2, StatusPaused); err != nil {
		t.Fatalf("seeding m2 as paused: %v", err)
	}

	updated, err := repo.PauseMarkets(ctx, []string{m1, m2})
	if err != nil {
		t.Fatalf("pausing with one already paused: %v", err)
	}
	// Both requested markets come back paused — the already-paused one is
	// included so callers can republish its config on retry.
	if len(updated) != 2 {
		t.Fatalf("updated len = %d, want 2", len(updated))
	}
	for _, market := range updated {
		if market.Status != StatusPaused {
			t.Errorf("market %s status = %s, want PAUSED", market.ID, market.Status)
		}
	}
}

func TestPGRepository_PauseMarkets_OneResolved_RollsBack(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, m1 := seedBinaryEvent(t, repo, "bp-res-1")
	_, m2 := seedBinaryEvent(t, repo, "bp-res-2")
	if _, err := repo.UpdateStatus(ctx, m2, StatusResolved); err != nil {
		t.Fatalf("seeding m2 as resolved: %v", err)
	}

	_, err := repo.PauseMarkets(ctx, []string{m1, m2})
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got: %v", err)
	}
	got, err := repo.GetMarket(ctx, m1)
	if err != nil {
		t.Fatalf("loading m1: %v", err)
	}
	if got.Status != StatusActive {
		t.Errorf("m1 status = %s, want ACTIVE (resolved m2 must not partially commit m1)",
			got.Status)
	}
}

func TestPGRepository_UpdateStatus_SameStatusIsIdempotent(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, marketID := seedBinaryEvent(t, repo, "idem-status")
	if _, err := repo.UpdateStatus(ctx, marketID, StatusPaused); err != nil {
		t.Fatalf("first pause: %v", err)
	}
	got, err := repo.UpdateStatus(ctx, marketID, StatusPaused)
	if err != nil {
		t.Fatalf("repeat pause should be a no-op, got: %v", err)
	}
	if got.Status != StatusPaused {
		t.Errorf("status = %s, want PAUSED", got.Status)
	}
}

func TestPGRepository_PauseMarkets_EmptyList(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)

	_, err := repo.PauseMarkets(context.Background(), nil)
	if !errors.Is(err, ErrInvalidMarket) {
		t.Errorf("expected ErrInvalidMarket, got: %v", err)
	}
}

func TestPGRepository_UpdateMarketPrices(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, marketID := seedBinaryEvent(t, repo, "prices")

	if err := repo.UpdateMarketPrices(ctx, marketID, 65, 35, 100000, 50000); err != nil {
		t.Fatalf("updating prices: %v", err)
	}
	got, err := repo.GetMarket(ctx, marketID)
	if err != nil {
		t.Fatalf("get market: %v", err)
	}
	if got.PriceYes != 65 || got.PriceNo != 35 {
		t.Errorf("prices = (%d, %d), want (65, 35)", got.PriceYes, got.PriceNo)
	}
}

func TestPGRepository_UpdateMarketPrices_NotFound(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	err := repo.UpdateMarketPrices(ctx, "00000000-0000-0000-0000-000000000000", 50, 50, 0, 0)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestPGRepository_UpdateMarketMetadata(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, marketID := seedBinaryEvent(t, repo, "meta")
	newQ := "Updated?"
	got, err := repo.UpdateMarketMetadata(ctx, marketID, &MarketUpdate{Question: &newQ})
	if err != nil {
		t.Fatalf("updating metadata: %v", err)
	}
	if got.Question != newQ {
		t.Errorf("question = %q, want %q", got.Question, newQ)
	}
}

func TestPGRepository_UpdateMarketMetadata_NotFound(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	q := "x"
	_, err := repo.UpdateMarketMetadata(ctx, "00000000-0000-0000-0000-000000000000", &MarketUpdate{Question: &q})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

// --- fee rate -----------------------------------------------------------

func TestPGRepository_UpdateFeeRate_SetThenUpdate(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, marketID := seedBinaryEvent(t, repo, "fee-update")

	got, err := repo.UpdateFeeRate(ctx, marketID, 25)
	if err != nil {
		t.Fatalf("first update: %v", err)
	}
	if got.FeeRateBps == nil || *got.FeeRateBps != 25 {
		t.Errorf("fee_rate_bps = %v, want 25", got.FeeRateBps)
	}

	got, err = repo.UpdateFeeRate(ctx, marketID, 75)
	if err != nil {
		t.Fatalf("second update: %v", err)
	}
	if got.FeeRateBps == nil || *got.FeeRateBps != 75 {
		t.Errorf("fee_rate_bps = %v, want 75", got.FeeRateBps)
	}
}

func TestPGRepository_UpdateFeeRate_MarketNotFound(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, err := repo.UpdateFeeRate(ctx, "00000000-0000-0000-0000-000000000000", 10)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestPGRepository_UpdateFeeRate_Invalid(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, marketID := seedBinaryEvent(t, repo, "fee-invalid")
	_, err := repo.UpdateFeeRate(ctx, marketID, -1)
	if !errors.Is(err, ErrInvalidFeeRate) {
		t.Errorf("expected ErrInvalidFeeRate, got: %v", err)
	}
}

// --- trading config ------------------------------------------------------

func TestPGRepository_UpdateTradingConfig_Roundtrip(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, marketID := seedBinaryEvent(t, repo, "trading-config")
	maxSize := int64(500)

	got, err := repo.UpdateTradingConfig(ctx, marketID, TickSize0_001, 10, &maxSize)
	if err != nil {
		t.Fatalf("updating trading config: %v", err)
	}
	if got.TickSize != TickSize0_001 {
		t.Errorf("tick_size = %s, want 0.001", got.TickSize)
	}
	if got.MinSize != 10 {
		t.Errorf("min_size = %d, want 10", got.MinSize)
	}
	if got.MaxSize == nil || *got.MaxSize != maxSize {
		t.Errorf("max_size = %v, want pointer to %d", got.MaxSize, maxSize)
	}
}

func TestPGRepository_UpdateTradingConfig_OutOfRange(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, marketID := seedBinaryEvent(t, repo, "trading-bad")
	_, err := repo.UpdateTradingConfig(ctx, marketID, TickSize(99), 10, nil)
	if !errors.Is(err, ErrInvalidMarket) {
		t.Errorf("expected ErrInvalidMarket, got: %v", err)
	}
}

func TestPGRepository_UpdateTradingConfig_NotFound(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, err := repo.UpdateTradingConfig(ctx, "00000000-0000-0000-0000-000000000000", TickSize0_01, 5, nil)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

// --- resolve / void ------------------------------------------------------

func TestPGRepository_ResolveMarketsInEvent_Partial(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	eventID, marketIDs := seedBinaryEventWithMarkets(t, repo, "partial", "partial-m1", "partial-m2")
	m1ID, m2ID := marketIDs[0], marketIDs[1]

	// Resolve only m1 = YES.
	updatedEvent, updatedMarkets, err := repo.ResolveMarketsInEvent(ctx, eventID, map[string]Outcome{
		m1ID: OutcomeYes,
	})
	if err != nil {
		t.Fatalf("partial resolve: %v", err)
	}
	if updatedEvent.Status != StatusActive {
		t.Errorf("event status after partial resolve = %s, want ACTIVE", updatedEvent.Status)
	}
	for _, market := range updatedMarkets {
		switch market.ID {
		case m1ID:
			if market.Status != StatusResolved {
				t.Errorf("m1 status = %s, want RESOLVED", market.Status)
			}
			if market.Outcome == nil || *market.Outcome != OutcomeYes {
				t.Errorf("m1 outcome = %v, want YES", market.Outcome)
			}
		case m2ID:
			if market.Status != StatusActive {
				t.Errorf("m2 status = %s, want ACTIVE", market.Status)
			}
		}
	}

	// Resolve m2 = NO. Event should auto-flip to RESOLVED.
	updatedEvent, _, err = repo.ResolveMarketsInEvent(ctx, eventID, map[string]Outcome{
		m2ID: OutcomeNo,
	})
	if err != nil {
		t.Fatalf("second resolve: %v", err)
	}
	if updatedEvent.Status != StatusResolved {
		t.Errorf("event status after full resolve = %s, want RESOLVED", updatedEvent.Status)
	}
}

func TestPGRepository_ResolveMarketsInEvent_RejectsForeignMarket(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	eventA, _ := seedBinaryEvent(t, repo, "event-a")
	_, foreignMarket := seedBinaryEvent(t, repo, "event-b")

	_, _, err := repo.ResolveMarketsInEvent(ctx, eventA, map[string]Outcome{
		foreignMarket: OutcomeYes,
	})
	if !errors.Is(err, ErrInvalidMarket) {
		t.Errorf("expected ErrInvalidMarket, got: %v", err)
	}
}

func TestPGRepository_ResolveMarketsInEvent_EmptyOutcomes(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	eventID, _ := seedBinaryEvent(t, repo, "empty-outcomes")

	_, _, err := repo.ResolveMarketsInEvent(ctx, eventID, map[string]Outcome{})
	if !errors.Is(err, ErrInvalidEvent) {
		t.Errorf("expected ErrInvalidEvent, got: %v", err)
	}
}

func TestPGRepository_ResolveMarketsInEvent_EventNotFound(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, _, err := repo.ResolveMarketsInEvent(ctx,
		"00000000-0000-0000-0000-000000000000",
		map[string]Outcome{"x": OutcomeYes})
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestPGRepository_VoidMarketsInEvent_Partial(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	eventID, marketIDs := seedBinaryEventWithMarkets(t, repo, "void", "void-m1", "void-m2")
	m1ID, m2ID := marketIDs[0], marketIDs[1]

	// Void m1 only.
	updatedEvent, _, err := repo.VoidMarketsInEvent(ctx, eventID, []string{m1ID})
	if err != nil {
		t.Fatalf("partial void: %v", err)
	}
	if updatedEvent.Status != StatusActive {
		t.Errorf("event status = %s, want ACTIVE (partial void)", updatedEvent.Status)
	}

	// Resolve m2 = YES. Mixed terminal => event auto-flips to RESOLVED.
	updatedEvent, _, err = repo.ResolveMarketsInEvent(ctx, eventID, map[string]Outcome{
		m2ID: OutcomeYes,
	})
	if err != nil {
		t.Fatalf("resolve after void: %v", err)
	}
	if updatedEvent.Status != StatusResolved {
		t.Errorf("event status = %s, want RESOLVED (mixed terminal)", updatedEvent.Status)
	}
}

func TestPGRepository_VoidMarketsInEvent_AllVoid(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	eventID, marketIDs := seedBinaryEventWithMarkets(t, repo, "all-void", "av-m1", "av-m2")

	updatedEvent, _, err := repo.VoidMarketsInEvent(ctx, eventID, marketIDs)
	if err != nil {
		t.Fatalf("voiding all: %v", err)
	}
	if updatedEvent.Status != StatusVoided {
		t.Errorf("event status = %s, want VOIDED", updatedEvent.Status)
	}
}

func TestPGRepository_ResolveMarketsInEvent_RetrySameOutcomeIsIdempotent(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	eventID, marketID := seedBinaryEvent(t, repo, "idem-resolve")
	outcomes := map[string]Outcome{marketID: OutcomeYes}

	if _, _, err := repo.ResolveMarketsInEvent(ctx, eventID, outcomes); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	_, markets, err := repo.ResolveMarketsInEvent(ctx, eventID, outcomes)
	if err != nil {
		t.Fatalf("retry with same outcome should be a no-op, got: %v", err)
	}
	if len(markets) != 1 || markets[0].Status != StatusResolved {
		t.Errorf("retry must still return the resolved market")
	}
	if markets[0].Outcome == nil || *markets[0].Outcome != OutcomeYes {
		t.Errorf("outcome = %v, want YES", markets[0].Outcome)
	}
}

func TestPGRepository_ResolveMarketsInEvent_RetryDifferentOutcomeConflicts(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	eventID, marketID := seedBinaryEvent(t, repo, "conflict-resolve")
	if _, _, err := repo.ResolveMarketsInEvent(ctx, eventID, map[string]Outcome{marketID: OutcomeYes}); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	_, _, err := repo.ResolveMarketsInEvent(ctx, eventID, map[string]Outcome{marketID: OutcomeNo})
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition for conflicting outcome, got: %v", err)
	}
}

func TestPGRepository_VoidMarketsInEvent_RetryIsIdempotent(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	eventID, marketID := seedBinaryEvent(t, repo, "idem-void")
	ids := []string{marketID}

	if _, _, err := repo.VoidMarketsInEvent(ctx, eventID, ids); err != nil {
		t.Fatalf("first void: %v", err)
	}
	updatedEvent, markets, err := repo.VoidMarketsInEvent(ctx, eventID, ids)
	if err != nil {
		t.Fatalf("retry void should be a no-op, got: %v", err)
	}
	if len(markets) != 1 || markets[0].Status != StatusVoided {
		t.Errorf("retry must still return the voided market")
	}
	if updatedEvent.Status != StatusVoided {
		t.Errorf("event status = %s, want VOIDED", updatedEvent.Status)
	}
}
