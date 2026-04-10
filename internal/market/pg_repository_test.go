//go:build integration

package market

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connecting to database: %v", err)
	}

	t.Cleanup(func() { pool.Close() })
	return pool
}

func cleanTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	// Truncate in reverse FK order.
	_, err := pool.Exec(ctx,
		`TRUNCATE markets, events, categories CASCADE`)
	if err != nil {
		t.Fatalf("cleaning tables: %v", err)
	}
}

func TestPGRepository_CreateAndGetCategory(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
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
	t.Parallel()
	pool := testPool(t)
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
	t.Parallel()
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, err := repo.GetCategory(ctx, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestPGRepository_CreateAndGetEvent(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	event := &Event{
		Slug:             "us-election-2024",
		Title:            "2024 US Presidential Election",
		Description:      "Who will win?",
		EventType:        EventTypeBinary,
		ResolutionConfig: json.RawMessage(`{}`),
		Status:           StatusActive,
		EndDate:          time.Now().Add(30 * 24 * time.Hour),
	}
	if err := repo.CreateEvent(ctx, event); err != nil {
		t.Fatalf("creating event: %v", err)
	}

	events, err := repo.ListEvents(ctx, nil)
	if err != nil {
		t.Fatalf("listing events: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("expected at least one event")
	}

	got, err := repo.GetEvent(ctx, events[0].ID)
	if err != nil {
		t.Fatalf("getting event: %v", err)
	}
	if got.Title != event.Title {
		t.Errorf("event title = %q, want %q", got.Title, event.Title)
	}
	if got.Slug != event.Slug {
		t.Errorf("event slug = %q, want %q", got.Slug, event.Slug)
	}
}

func TestPGRepository_GetEventBySlug(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	event := &Event{
		Slug:             "slug-lookup-test",
		Title:            "Slug Lookup",
		EventType:        EventTypeBinary,
		ResolutionConfig: json.RawMessage(`{}`),
		Status:           StatusActive,
		EndDate:          time.Now().Add(24 * time.Hour),
	}
	if err := repo.CreateEvent(ctx, event); err != nil {
		t.Fatalf("creating event: %v", err)
	}

	got, err := repo.GetEventBySlug(ctx, "slug-lookup-test")
	if err != nil {
		t.Fatalf("getting event by slug: %v", err)
	}
	if got.Title != "Slug Lookup" {
		t.Errorf("event title = %q, want %q", got.Title, "Slug Lookup")
	}
}

func TestPGRepository_CreateEvent_DuplicateSlug(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	event := &Event{
		Slug:             "dup-event",
		Title:            "Original",
		EventType:        EventTypeBinary,
		ResolutionConfig: json.RawMessage(`{}`),
		Status:           StatusActive,
		EndDate:          time.Now().Add(24 * time.Hour),
	}
	if err := repo.CreateEvent(ctx, event); err != nil {
		t.Fatalf("creating event: %v", err)
	}

	event.Title = "Duplicate"
	err := repo.CreateEvent(ctx, event)
	if !errors.Is(err, ErrDuplicateSlug) {
		t.Errorf("expected ErrDuplicateSlug, got: %v", err)
	}
}

func TestPGRepository_GetEvent_NotFound(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, err := repo.GetEvent(ctx, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestPGRepository_ListEvents_StatusFilter(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	for _, slug := range []string{"active-event", "paused-event"} {
		status := StatusActive
		if slug == "paused-event" {
			status = StatusPaused
		}
		event := &Event{
			Slug:             slug,
			Title:            slug,
			EventType:        EventTypeBinary,
			ResolutionConfig: json.RawMessage(`{}`),
			Status:           status,
			EndDate:          time.Now().Add(24 * time.Hour),
		}
		if err := repo.CreateEvent(ctx, event); err != nil {
			t.Fatalf("creating event %s: %v", slug, err)
		}
	}

	active, err := repo.ListEvents(ctx, []Status{StatusActive})
	if err != nil {
		t.Fatalf("listing active events: %v", err)
	}
	if len(active) != 1 {
		t.Errorf("expected 1 active event, got %d", len(active))
	}

	all, err := repo.ListEvents(ctx, nil)
	if err != nil {
		t.Fatalf("listing all events: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 total events, got %d", len(all))
	}
}

func TestPGRepository_CreateAndGetMarket(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	market := &Market{
		Slug:            "will-it-rain",
		Question:        "Will it rain tomorrow?",
		OutcomeYesLabel: "Yes",
		OutcomeNoLabel:  "No",
		TokenIDYes:      "token-yes",
		TokenIDNo:       "token-no",
		ConditionID:     "condition-1",
		Status:          StatusActive,
		PriceYes:        50,
		PriceNo:         50,
	}
	if err := repo.CreateMarket(ctx, market); err != nil {
		t.Fatalf("creating market: %v", err)
	}

	markets, err := repo.ListMarkets(ctx, nil)
	if err != nil {
		t.Fatalf("listing markets: %v", err)
	}
	if len(markets) == 0 {
		t.Fatal("expected at least one market")
	}

	got, err := repo.GetMarket(ctx, markets[0].ID)
	if err != nil {
		t.Fatalf("getting market: %v", err)
	}
	if got.Question != market.Question {
		t.Errorf("market question = %q, want %q", got.Question, market.Question)
	}
}

func TestPGRepository_GetMarketBySlug(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	market := &Market{
		Slug:            "slug-market-test",
		Question:        "Test?",
		OutcomeYesLabel: "Yes",
		OutcomeNoLabel:  "No",
		TokenIDYes:      "ty",
		TokenIDNo:       "tn",
		ConditionID:     "c1",
		Status:          StatusActive,
		PriceYes:        50,
		PriceNo:         50,
	}
	if err := repo.CreateMarket(ctx, market); err != nil {
		t.Fatalf("creating market: %v", err)
	}

	got, err := repo.GetMarketBySlug(ctx, "slug-market-test")
	if err != nil {
		t.Fatalf("getting market by slug: %v", err)
	}
	if got.Question != "Test?" {
		t.Errorf("market question = %q, want %q", got.Question, "Test?")
	}
}

func TestPGRepository_UpdateStatus(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	market := &Market{
		Slug:            "status-test",
		Question:        "Status?",
		OutcomeYesLabel: "Yes",
		OutcomeNoLabel:  "No",
		TokenIDYes:      "ty",
		TokenIDNo:       "tn",
		ConditionID:     "c1",
		Status:          StatusActive,
		PriceYes:        50,
		PriceNo:         50,
	}
	if err := repo.CreateMarket(ctx, market); err != nil {
		t.Fatalf("creating market: %v", err)
	}

	markets, err := repo.ListMarkets(ctx, nil)
	if err != nil {
		t.Fatalf("listing markets: %v", err)
	}
	id := markets[0].ID

	if err := repo.UpdateStatus(ctx, id, StatusPaused); err != nil {
		t.Fatalf("updating status to paused: %v", err)
	}

	got, err := repo.GetMarket(ctx, id)
	if err != nil {
		t.Fatalf("getting market: %v", err)
	}
	if got.Status != StatusPaused {
		t.Errorf("market status = %s, want %s", got.Status, StatusPaused)
	}
}

func TestPGRepository_UpdateStatus_InvalidTransition(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	market := &Market{
		Slug:            "invalid-transition",
		Question:        "Transition?",
		OutcomeYesLabel: "Yes",
		OutcomeNoLabel:  "No",
		TokenIDYes:      "ty",
		TokenIDNo:       "tn",
		ConditionID:     "c1",
		Status:          StatusActive,
		PriceYes:        50,
		PriceNo:         50,
	}
	if err := repo.CreateMarket(ctx, market); err != nil {
		t.Fatalf("creating market: %v", err)
	}

	// Resolve the market first.
	markets, _ := repo.ListMarkets(ctx, nil)
	id := markets[0].ID
	if err := repo.UpdateStatus(ctx, id, StatusResolved); err != nil {
		t.Fatalf("resolving market: %v", err)
	}

	// Try to go back to active — should fail.
	err := repo.UpdateStatus(ctx, id, StatusActive)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got: %v", err)
	}
}

func TestPGRepository_UpdateMarketPrices(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	market := &Market{
		Slug:            "price-test",
		Question:        "Prices?",
		OutcomeYesLabel: "Yes",
		OutcomeNoLabel:  "No",
		TokenIDYes:      "ty",
		TokenIDNo:       "tn",
		ConditionID:     "c1",
		Status:          StatusActive,
		PriceYes:        50,
		PriceNo:         50,
	}
	if err := repo.CreateMarket(ctx, market); err != nil {
		t.Fatalf("creating market: %v", err)
	}

	markets, _ := repo.ListMarkets(ctx, nil)
	id := markets[0].ID

	if err := repo.UpdateMarketPrices(ctx, id, 65, 35, 100000, 50000); err != nil {
		t.Fatalf("updating prices: %v", err)
	}

	got, err := repo.GetMarket(ctx, id)
	if err != nil {
		t.Fatalf("getting market: %v", err)
	}
	if got.PriceYes != 65 {
		t.Errorf("price_yes = %d, want 65", got.PriceYes)
	}
	if got.PriceNo != 35 {
		t.Errorf("price_no = %d, want 35", got.PriceNo)
	}
	if got.Volume != 100000 {
		t.Errorf("volume = %d, want 100000", got.Volume)
	}
	if got.OpenInterest != 50000 {
		t.Errorf("open_interest = %d, want 50000", got.OpenInterest)
	}
}

func TestPGRepository_UpdateMarketPrices_NotFound(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	err := repo.UpdateMarketPrices(ctx, "00000000-0000-0000-0000-000000000000", 50, 50, 0, 0)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}
