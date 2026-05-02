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
	// Truncate in reverse FK order.
	_, err := pool.Exec(ctx,
		`TRUNCATE markets, events, categories CASCADE`)
	if err != nil {
		t.Fatalf("cleaning tables: %v", err)
	}
}

// seedCategory creates a category with a unique slug and returns its ID.
// Events require a non-null category_id, so every event-creating test needs
// a fixture like this.
func seedCategory(t *testing.T, repo *PGRepository, slug string) string {
	t.Helper()
	cat := &Category{Name: slug, Slug: slug}
	if err := repo.CreateCategory(context.Background(), cat); err != nil {
		t.Fatalf("seeding category %q: %v", slug, err)
	}
	return cat.ID
}

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

func TestPGRepository_CreateAndGetEvent(t *testing.T) {
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
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	catID := seedCategory(t, repo, "general")
	event := &Event{
		Slug:             "slug-lookup-test",
		Title:            "Slug Lookup",
		CategoryID:       catID,
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
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	catID := seedCategory(t, repo, "general")
	event := &Event{
		Slug:             "dup-event",
		Title:            "Original",
		CategoryID:       catID,
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
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, err := repo.GetEvent(ctx, "00000000-0000-0000-0000-000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestPGRepository_ListEvents_StatusFilter(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	catID := seedCategory(t, repo, "general")
	for _, slug := range []string{"active-event", "paused-event"} {
		status := StatusActive
		if slug == "paused-event" {
			status = StatusPaused
		}
		event := &Event{
			Slug:             slug,
			Title:            slug,
			CategoryID:       catID,
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
	pool := postgres.TestPool(t)
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
	pool := postgres.TestPool(t)
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
	pool := postgres.TestPool(t)
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

	updated, err := repo.UpdateStatus(ctx, id, StatusPaused)
	if err != nil {
		t.Fatalf("updating status to paused: %v", err)
	}
	if updated.Status != StatusPaused {
		t.Errorf("returned market status = %s, want %s", updated.Status, StatusPaused)
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
	pool := postgres.TestPool(t)
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
	if _, err := repo.UpdateStatus(ctx, id, StatusResolved); err != nil {
		t.Fatalf("resolving market: %v", err)
	}

	// Try to go back to active — should fail.
	_, err := repo.UpdateStatus(ctx, id, StatusActive)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("expected ErrInvalidTransition, got: %v", err)
	}
}

func TestPGRepository_UpdateMarketPrices(t *testing.T) {
	pool := postgres.TestPool(t)
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
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	err := repo.UpdateMarketPrices(ctx, "00000000-0000-0000-0000-000000000000", 50, 50, 0, 0)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestPGRepository_UpdateEvent(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	origCat := seedCategory(t, repo, "sports")
	newCat := seedCategory(t, repo, "politics")

	event := &Event{
		Slug:             "updatable-event",
		Title:            "Original",
		Description:      "Original description",
		CategoryID:       origCat,
		EventType:        EventTypeBinary,
		ResolutionConfig: json.RawMessage(`{}`),
		Status:           StatusActive,
		EndDate:          time.Now().Add(30 * 24 * time.Hour),
	}
	if err := repo.CreateEvent(ctx, event); err != nil {
		t.Fatalf("creating event: %v", err)
	}
	events, _ := repo.ListEvents(ctx, nil)
	id := events[0].ID

	newTitle := "Updated Title"
	featured := true
	got, err := repo.UpdateEvent(ctx, id, &EventUpdate{
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
	if got.Description != "Original description" {
		t.Errorf("description unexpectedly changed: %q", got.Description)
	}
	if got.CategoryID != newCat {
		t.Errorf("category_id = %q, want %q", got.CategoryID, newCat)
	}
	if !got.Featured {
		t.Error("featured = false, want true")
	}

	// Partial update: only title changes; category remains newCat.
	newerTitle := "Another Update"
	unchanged, err := repo.UpdateEvent(ctx, id, &EventUpdate{Title: &newerTitle})
	if err != nil {
		t.Fatalf("partial update: %v", err)
	}
	if unchanged.Title != newerTitle {
		t.Errorf("title = %q, want %q", unchanged.Title, newerTitle)
	}
	if unchanged.CategoryID != newCat {
		t.Errorf("category changed during partial update: %q, want %q", unchanged.CategoryID, newCat)
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

func TestPGRepository_UpdateMarketMetadata(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	market := &Market{
		Slug:            "metadata-test",
		Question:        "Original?",
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

	newQuestion := "Updated?"
	newYes := "Absolutely"
	got, err := repo.UpdateMarketMetadata(ctx, id, &MarketUpdate{
		Question:        &newQuestion,
		OutcomeYesLabel: &newYes,
	})
	if err != nil {
		t.Fatalf("updating market metadata: %v", err)
	}
	if got.Question != newQuestion {
		t.Errorf("question = %q, want %q", got.Question, newQuestion)
	}
	if got.OutcomeYesLabel != newYes {
		t.Errorf("yes label = %q, want %q", got.OutcomeYesLabel, newYes)
	}
	if got.OutcomeNoLabel != "No" {
		t.Errorf("no label unexpectedly changed: %q", got.OutcomeNoLabel)
	}
	if got.Status != StatusActive {
		t.Errorf("status unexpectedly changed: %s", got.Status)
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

// seedMarketForFeeRate creates the category+market fixture chain that
// market_fee_rates' FK requires, and returns the market id.
func seedMarketForFeeRate(t *testing.T, repo *PGRepository, slug string) string {
	t.Helper()
	ctx := context.Background()
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
	if err := repo.CreateEvent(ctx, event); err != nil {
		t.Fatalf("seeding event: %v", err)
	}
	mkt := &Market{
		Slug:            slug,
		Question:        "Q?",
		OutcomeYesLabel: "Yes",
		OutcomeNoLabel:  "No",
		TokenIDYes:      "ty-" + slug,
		TokenIDNo:       "tn-" + slug,
		ConditionID:     "c-" + slug,
		Status:          StatusActive,
		PriceYes:        50,
		PriceNo:         50,
	}
	if err := repo.CreateMarket(ctx, mkt); err != nil {
		t.Fatalf("seeding market: %v", err)
	}
	markets, err := repo.ListMarkets(ctx, nil)
	if err != nil || len(markets) == 0 {
		t.Fatalf("listing markets: %v", err)
	}
	return markets[0].ID
}

func TestPGRepository_UpdateFeeRate_SetThenUpdate(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	marketID := seedMarketForFeeRate(t, repo, "fee-update")

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

	read, err := repo.GetMarket(ctx, marketID)
	if err != nil {
		t.Fatalf("get market: %v", err)
	}
	if read.FeeRateBps == nil || *read.FeeRateBps != 75 {
		t.Errorf("read fee_rate_bps = %v, want 75", read.FeeRateBps)
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

	marketID := seedMarketForFeeRate(t, repo, "fee-invalid")

	_, err := repo.UpdateFeeRate(ctx, marketID, -1)
	if !errors.Is(err, ErrInvalidFeeRate) {
		t.Errorf("expected ErrInvalidFeeRate, got: %v", err)
	}
}
