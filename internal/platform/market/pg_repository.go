package market

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Shisa-Fosho/services/internal/shared/postgres"
)

// PGRepository implements Repository using PostgreSQL via pgx.
type PGRepository struct {
	pool *pgxpool.Pool
}

// NewPGRepository creates a new PostgreSQL-backed repository.
func NewPGRepository(pool *pgxpool.Pool) *PGRepository {
	return &PGRepository{pool: pool}
}

// CreateCategory persists a new category and populates cat.ID with the
// generated UUID. Returns ErrDuplicateSlug if the slug already exists.
func (repo *PGRepository) CreateCategory(ctx context.Context, cat *Category) error {
	err := repo.pool.QueryRow(ctx,
		`INSERT INTO categories (name, slug) VALUES ($1, $2) RETURNING id`,
		cat.Name, cat.Slug,
	).Scan(&cat.ID)
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			return fmt.Errorf("creating category %q: %w", cat.Slug, ErrDuplicateSlug)
		}
		return fmt.Errorf("creating category: %w", err)
	}
	return nil
}

// GetCategory retrieves a category by ID. Returns ErrNotFound if not found.
func (repo *PGRepository) GetCategory(ctx context.Context, id string) (*Category, error) {
	rows, err := repo.pool.Query(ctx, `SELECT * FROM categories WHERE id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("getting category %s: %w", id, err)
	}
	category, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[Category])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("getting category %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("getting category %s: %w", id, err)
	}
	return category, nil
}

// ListCategories returns all categories ordered by name.
func (repo *PGRepository) ListCategories(ctx context.Context) ([]*Category, error) {
	rows, err := repo.pool.Query(ctx, `SELECT * FROM categories ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("listing categories: %w", err)
	}
	categories, err := pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[Category])
	if err != nil {
		return nil, fmt.Errorf("scanning categories: %w", err)
	}
	return categories, nil
}

// UpdateCategory changes the name and slug of an existing category and
// returns the updated row in a single roundtrip via RETURNING. Returns
// ErrNotFound if the id doesn't match a row, or ErrDuplicateSlug if the new
// slug is already taken by another category.
func (repo *PGRepository) UpdateCategory(ctx context.Context, id, name, slug string) (*Category, error) {
	cat := &Category{}
	err := repo.pool.QueryRow(ctx,
		`UPDATE categories SET name = $1, slug = $2 WHERE id = $3
		 RETURNING id, name, slug`,
		name, slug, id,
	).Scan(&cat.ID, &cat.Name, &cat.Slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("updating category %s: %w", id, ErrNotFound)
		}
		if postgres.IsUniqueViolation(err) {
			return nil, fmt.Errorf("updating category %s: %w", id, ErrDuplicateSlug)
		}
		return nil, fmt.Errorf("updating category %s: %w", id, err)
	}
	return cat, nil
}

// DeleteCategory removes a category by id. Returns ErrNotFound if the id
// doesn't match a row.
func (repo *PGRepository) DeleteCategory(ctx context.Context, id string) error {
	tag, err := repo.pool.Exec(ctx,
		`DELETE FROM categories WHERE id = $1`, id,
	)
	if err != nil {
		return fmt.Errorf("deleting category %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("deleting category %s: %w", id, ErrNotFound)
	}
	return nil
}

// CreateEventWithMarkets persists an event together with its constituent
// markets in a single transaction. Both the event-level and per-market
// validators run before any SQL executes; the transaction then either
// commits all rows or rolls back fully.
func (repo *PGRepository) CreateEventWithMarkets(ctx context.Context, event *Event, markets []*Market) (*Event, []*Market, error) {
	if event == nil {
		return nil, nil, fmt.Errorf("event is nil: %w", ErrInvalidEvent)
	}
	if len(markets) == 0 {
		return nil, nil, fmt.Errorf("at least one market is required: %w", ErrInvalidEvent)
	}
	if err := ValidateEvent(event, time.Now()); err != nil {
		return nil, nil, fmt.Errorf("creating event: %w", err)
	}
	// All markets get Status=Active regardless of caller intent. EventID is
	// validated later (after the event insert assigns it) — checking it
	// here would be a chicken-and-egg problem with CreateEventWithMarkets.
	for _, market := range markets {
		market.Status = StatusActive
	}
	if err := ValidateNegRiskCoherence(event, markets); err != nil {
		return nil, nil, fmt.Errorf("creating event: %w", err)
	}

	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("creating event: beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	eventRows, err := tx.Query(ctx,
		`INSERT INTO events (
			slug, title, description, category_id, event_type,
			resolution_config, status, end_date, featured, featured_sort_order,
			neg_risk_market_id
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		RETURNING *`,
		event.Slug, event.Title, event.Description, event.CategoryID,
		event.EventType, event.ResolutionConfig, event.Status,
		event.EndDate, event.Featured, event.FeaturedSortOrder,
		event.NegRiskMarketID,
	)
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			return nil, nil, fmt.Errorf("creating event %q: %w", event.Slug, ErrDuplicateSlug)
		}
		return nil, nil, fmt.Errorf("creating event: %w", err)
	}
	createdEvent, err := pgx.CollectOneRow(eventRows, pgx.RowToAddrOfStructByName[Event])
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			return nil, nil, fmt.Errorf("creating event %q: %w", event.Slug, ErrDuplicateSlug)
		}
		return nil, nil, fmt.Errorf("scanning created event: %w", err)
	}

	createdMarkets := make([]*Market, 0, len(markets))
	for _, market := range markets {
		market.EventID = createdEvent.ID
		if err := ValidateMarket(market); err != nil {
			return nil, nil, fmt.Errorf("validating market %q: %w", market.Slug, err)
		}
		marketRows, err := tx.Query(ctx,
			`INSERT INTO markets (
				slug, event_id, question, outcome_yes_label, outcome_no_label,
				token_id_yes, token_id_no, condition_id, question_id,
				status, outcome, price_yes, price_no, volume, open_interest,
				fee_rate_bps, tick_size, min_size, max_size
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19)
			RETURNING *`,
			market.Slug, createdEvent.ID, market.Question,
			market.OutcomeYesLabel, market.OutcomeNoLabel,
			market.TokenIDYes, market.TokenIDNo, market.ConditionID, market.QuestionID,
			market.Status, market.Outcome, market.PriceYes, market.PriceNo,
			market.Volume, market.OpenInterest,
			market.FeeRateBps, market.TickSize, market.MinSize, market.MaxSize,
		)
		if err != nil {
			if postgres.IsUniqueViolation(err) {
				return nil, nil, fmt.Errorf("creating market %q: %w", market.Slug, ErrDuplicateSlug)
			}
			return nil, nil, fmt.Errorf("creating market %q: %w", market.Slug, err)
		}
		createdMarket, err := pgx.CollectOneRow(marketRows, pgx.RowToAddrOfStructByName[Market])
		if err != nil {
			if postgres.IsUniqueViolation(err) {
				return nil, nil, fmt.Errorf("creating market %q: %w", market.Slug, ErrDuplicateSlug)
			}
			return nil, nil, fmt.Errorf("scanning created market %q: %w", market.Slug, err)
		}
		createdMarkets = append(createdMarkets, createdMarket)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("creating event: committing: %w", err)
	}
	return createdEvent, createdMarkets, nil
}

// GetEvent retrieves an event by ID. Returns ErrNotFound if not found.
func (repo *PGRepository) GetEvent(ctx context.Context, id string) (*Event, error) {
	rows, err := repo.pool.Query(ctx, `SELECT * FROM events WHERE id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("getting event %s: %w", id, err)
	}
	event, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[Event])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("getting event %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("getting event %s: %w", id, err)
	}
	return event, nil
}

// GetEventBySlug retrieves an event by slug. Returns ErrNotFound if not found.
func (repo *PGRepository) GetEventBySlug(ctx context.Context, slug string) (*Event, error) {
	rows, err := repo.pool.Query(ctx, `SELECT * FROM events WHERE slug = $1`, slug)
	if err != nil {
		return nil, fmt.Errorf("getting event by slug %q: %w", slug, err)
	}
	event, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[Event])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("getting event by slug %q: %w", slug, ErrNotFound)
		}
		return nil, fmt.Errorf("getting event by slug %q: %w", slug, err)
	}
	return event, nil
}

// ListEvents returns events optionally filtered by statuses.
func (repo *PGRepository) ListEvents(ctx context.Context, statuses []Status) ([]*Event, error) {
	var rows pgx.Rows
	var err error

	if len(statuses) == 0 {
		rows, err = repo.pool.Query(ctx,
			`SELECT * FROM events ORDER BY created_at DESC`,
		)
	} else {
		rows, err = repo.pool.Query(ctx,
			`SELECT * FROM events WHERE status = ANY($1) ORDER BY created_at DESC`,
			statusSlice(statuses),
		)
	}
	if err != nil {
		return nil, fmt.Errorf("listing events: %w", err)
	}
	events, err := pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[Event])
	if err != nil {
		return nil, fmt.Errorf("scanning events: %w", err)
	}
	return events, nil
}

// UpdateEvent applies a partial update to an event's metadata and returns
// the resulting row. Any argument bound as SQL NULL (i.e. a nil pointer on
// the EventUpdate) falls through COALESCE to the existing column value, so
// only fields the admin set are actually changed.
func (repo *PGRepository) UpdateEvent(ctx context.Context, id string, update *EventUpdate) (*Event, error) {
	if err := ValidateEventUpdate(update); err != nil {
		return nil, fmt.Errorf("updating event %s: %w", id, err)
	}
	rows, err := repo.pool.Query(ctx,
		`UPDATE events SET
		    title               = COALESCE($1, title),
		    description         = COALESCE($2, description),
		    category_id         = COALESCE($3, category_id),
		    featured            = COALESCE($4, featured),
		    featured_sort_order = COALESCE($5, featured_sort_order),
		    updated_at          = now()
		 WHERE id = $6
		 RETURNING *`,
		update.Title, update.Description, update.CategoryID,
		update.Featured, update.FeaturedSortOrder, id,
	)
	if err != nil {
		return nil, fmt.Errorf("updating event %s: %w", id, err)
	}
	event, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[Event])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("updating event %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("updating event %s: %w", id, err)
	}
	return event, nil
}

// GetMarket retrieves a market by ID. Returns ErrNotFound if not found.
func (repo *PGRepository) GetMarket(ctx context.Context, id string) (*Market, error) {
	rows, err := repo.pool.Query(ctx, `SELECT * FROM markets WHERE id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("getting market %s: %w", id, err)
	}
	market, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[Market])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("getting market %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("getting market %s: %w", id, err)
	}
	return market, nil
}

// GetMarketBySlug retrieves a market by slug. Returns ErrNotFound if not found.
func (repo *PGRepository) GetMarketBySlug(ctx context.Context, slug string) (*Market, error) {
	rows, err := repo.pool.Query(ctx, `SELECT * FROM markets WHERE slug = $1`, slug)
	if err != nil {
		return nil, fmt.Errorf("getting market by slug %q: %w", slug, err)
	}
	market, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[Market])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("getting market by slug %q: %w", slug, ErrNotFound)
		}
		return nil, fmt.Errorf("getting market by slug %q: %w", slug, err)
	}
	return market, nil
}

// ListMarkets returns markets optionally filtered by statuses.
func (repo *PGRepository) ListMarkets(ctx context.Context, statuses []Status) ([]*Market, error) {
	var rows pgx.Rows
	var err error

	if len(statuses) == 0 {
		rows, err = repo.pool.Query(ctx,
			`SELECT * FROM markets ORDER BY created_at DESC`,
		)
	} else {
		rows, err = repo.pool.Query(ctx,
			`SELECT * FROM markets WHERE status = ANY($1) ORDER BY created_at DESC`,
			statusSlice(statuses),
		)
	}
	if err != nil {
		return nil, fmt.Errorf("listing markets: %w", err)
	}
	markets, err := pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[Market])
	if err != nil {
		return nil, fmt.Errorf("scanning markets: %w", err)
	}
	return markets, nil
}

// ListMarketsByEvent returns all markets belonging to an event.
func (repo *PGRepository) ListMarketsByEvent(ctx context.Context, eventID string) ([]*Market, error) {
	rows, err := repo.pool.Query(ctx,
		`SELECT * FROM markets WHERE event_id = $1 ORDER BY created_at DESC`,
		eventID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing markets for event %s: %w", eventID, err)
	}
	markets, err := pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[Market])
	if err != nil {
		return nil, fmt.Errorf("scanning markets: %w", err)
	}
	return markets, nil
}

// UpdateMarketMetadata applies a partial update to a market's editable
// fields and returns the resulting row. Any argument bound as SQL NULL
// falls through COALESCE to the existing column value.
func (repo *PGRepository) UpdateMarketMetadata(ctx context.Context, id string, update *MarketUpdate) (*Market, error) {
	if err := ValidateMarketUpdate(update); err != nil {
		return nil, fmt.Errorf("updating market %s: %w", id, err)
	}
	rows, err := repo.pool.Query(ctx,
		`UPDATE markets SET
		    question          = COALESCE($1, question),
		    outcome_yes_label = COALESCE($2, outcome_yes_label),
		    outcome_no_label  = COALESCE($3, outcome_no_label),
		    updated_at        = now()
		 WHERE id = $4
		 RETURNING *`,
		update.Question, update.OutcomeYesLabel, update.OutcomeNoLabel, id,
	)
	if err != nil {
		return nil, fmt.Errorf("updating market %s: %w", id, err)
	}
	market, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[Market])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("updating market %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("updating market %s: %w", id, err)
	}
	return market, nil
}

// UpdateStatus changes the status of a market. Validates the transition
// inside a transaction holding a row lock, then returns the updated row
// via UPDATE ... RETURNING — no extra round-trip.
//
// Idempotent: a market already in the requested status is returned as-is
// rather than rejected, so a caller retrying after a failed downstream
// publish reaches the publish step again instead of getting a conflict.
func (repo *PGRepository) UpdateStatus(ctx context.Context, id string, status Status) (*Market, error) {
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("updating market status: beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var current Status
	err = tx.QueryRow(ctx,
		`SELECT status FROM markets WHERE id = $1 FOR UPDATE`, id,
	).Scan(&current)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("updating market %s status: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("updating market status: reading current: %w", err)
	}

	if current == status {
		rows, err := tx.Query(ctx, `SELECT * FROM markets WHERE id = $1`, id)
		if err != nil {
			return nil, fmt.Errorf("re-reading market %s: %w", id, err)
		}
		market, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[Market])
		if err != nil {
			return nil, fmt.Errorf("re-reading market %s: %w", id, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, fmt.Errorf("updating market status: committing: %w", err)
		}
		return market, nil
	}

	if err := ValidateStatusTransition(current, status); err != nil {
		return nil, err
	}

	rows, err := tx.Query(ctx,
		`UPDATE markets SET status = $1, updated_at = now() WHERE id = $2 RETURNING *`,
		status, id,
	)
	if err != nil {
		return nil, fmt.Errorf("updating market %s status: %w", id, err)
	}
	market, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[Market])
	if err != nil {
		return nil, fmt.Errorf("updating market %s status: %w", id, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("updating market status: committing: %w", err)
	}
	return market, nil
}

// PauseMarkets atomically transitions every requested market from Active
// to Paused. See repository.Repository for the full contract.
//
// Lock order: markets are locked in sorted-ID order via ORDER BY id so
// concurrent bulk operations on overlapping ID sets serialize cleanly
// instead of deadlocking.
func (repo *PGRepository) PauseMarkets(ctx context.Context, marketIDs []string) ([]*Market, error) {
	if len(marketIDs) == 0 {
		return nil, fmt.Errorf("market_ids is empty: %w", ErrInvalidMarket)
	}
	sorted := uniqueSorted(marketIDs)

	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("pausing markets: beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	lockRows, err := tx.Query(ctx,
		`SELECT id, status FROM markets WHERE id = ANY($1) ORDER BY id FOR UPDATE`,
		sorted,
	)
	if err != nil {
		return nil, fmt.Errorf("locking markets: %w", err)
	}
	found := make(map[string]Status, len(sorted))
	for lockRows.Next() {
		var id string
		var status Status
		if err := lockRows.Scan(&id, &status); err != nil {
			lockRows.Close()
			return nil, fmt.Errorf("scanning locked row: %w", err)
		}
		found[id] = status
	}
	lockRows.Close()
	if err := lockRows.Err(); err != nil {
		return nil, fmt.Errorf("iterating locked rows: %w", err)
	}

	var missing []string
	for _, id := range sorted {
		if _, ok := found[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("markets not found: %s: %w",
			strings.Join(missing, ", "), ErrNotFound)
	}

	// Already-Paused markets are idempotent no-ops (a retry after a failed
	// publish must succeed); any other non-Active status is a real conflict.
	var pending []string
	var invalid []string
	for _, id := range sorted {
		if found[id] == StatusPaused {
			continue
		}
		if err := ValidateStatusTransition(found[id], StatusPaused); err != nil {
			invalid = append(invalid, fmt.Sprintf("%s (status=%s)", id, found[id].String()))
			continue
		}
		pending = append(pending, id)
	}
	if len(invalid) > 0 {
		return nil, fmt.Errorf("markets not in Active status: %s: %w",
			strings.Join(invalid, ", "), ErrInvalidTransition)
	}

	if len(pending) > 0 {
		if _, err := tx.Exec(ctx,
			`UPDATE markets SET status = $1, updated_at = now() WHERE id = ANY($2)`,
			StatusPaused, pending,
		); err != nil {
			return nil, fmt.Errorf("updating markets to Paused: %w", err)
		}
	}

	// Return every requested market (not just the freshly updated ones) so
	// the caller republishes config for already-paused markets on retry.
	readRows, err := tx.Query(ctx,
		`SELECT * FROM markets WHERE id = ANY($1) ORDER BY id`, sorted,
	)
	if err != nil {
		return nil, fmt.Errorf("re-reading paused markets: %w", err)
	}
	markets, err := pgx.CollectRows(readRows, pgx.RowToAddrOfStructByName[Market])
	if err != nil {
		return nil, fmt.Errorf("collecting paused markets: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("pausing markets: committing: %w", err)
	}
	return markets, nil
}

// uniqueSorted returns ids deduplicated and sorted ascending.
func uniqueSorted(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// UpdateMarketPrices updates the current prices, volume, and open interest.
func (repo *PGRepository) UpdateMarketPrices(ctx context.Context, id string, priceYes, priceNo, volume, openInterest int64) error {
	tag, err := repo.pool.Exec(ctx,
		`UPDATE markets SET price_yes = $1, price_no = $2, volume = $3,
		 open_interest = $4, updated_at = now() WHERE id = $5`,
		priceYes, priceNo, volume, openInterest, id,
	)
	if err != nil {
		return fmt.Errorf("updating market %s prices: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("updating market %s prices: %w", id, ErrNotFound)
	}
	return nil
}

// UpdateFeeRate validates and writes a market's fee rate, returning the
// updated row. Returns ErrInvalidFeeRate for out-of-range bps and
// ErrNotFound if the market does not exist.
func (repo *PGRepository) UpdateFeeRate(ctx context.Context, marketID string, bps int) (*Market, error) {
	if err := ValidateFeeRateBps(bps); err != nil {
		return nil, fmt.Errorf("updating fee rate: %w", err)
	}
	rows, err := repo.pool.Query(ctx,
		`UPDATE markets
		 SET    fee_rate_bps = $1,
		        updated_at   = now()
		 WHERE  id = $2
		 RETURNING *`,
		bps, marketID,
	)
	if err != nil {
		return nil, fmt.Errorf("updating fee rate for market %s: %w", marketID, err)
	}
	market, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[Market])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("updating fee rate for market %s: %w", marketID, ErrNotFound)
		}
		return nil, fmt.Errorf("updating fee rate for market %s: %w", marketID, err)
	}
	return market, nil
}

// UpdateTradingConfig writes tick_size/min_size/max_size onto a market and
// returns the updated row. Validates the update before issuing SQL.
func (repo *PGRepository) UpdateTradingConfig(ctx context.Context, marketID string, tickSize TickSize, minSize int64, maxSize *int64) (*Market, error) {
	if err := ValidateTradingConfigUpdate(tickSize, minSize, maxSize); err != nil {
		return nil, fmt.Errorf("updating trading config: %w", err)
	}
	rows, err := repo.pool.Query(ctx,
		`UPDATE markets
		 SET    tick_size = $1,
		        min_size  = $2,
		        max_size  = $3,
		        updated_at = now()
		 WHERE  id = $4
		 RETURNING *`,
		tickSize, minSize, maxSize, marketID,
	)
	if err != nil {
		return nil, fmt.Errorf("updating trading config for market %s: %w", marketID, err)
	}
	market, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[Market])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("updating trading config for market %s: %w", marketID, ErrNotFound)
		}
		return nil, fmt.Errorf("updating trading config for market %s: %w", marketID, err)
	}
	return market, nil
}

// ResolveMarketsInEvent atomically transitions the listed markets to
// Resolved with their declared outcomes. See repository.Repository for
// the full contract.
func (repo *PGRepository) ResolveMarketsInEvent(ctx context.Context, eventID string, outcomes map[string]Outcome) (*Event, []*Market, error) {
	if len(outcomes) == 0 {
		return nil, nil, fmt.Errorf("outcomes is empty: %w", ErrInvalidEvent)
	}
	return repo.transitionMarketsInEvent(ctx, eventID, transitionSpec{
		targetStatus:   StatusResolved,
		resolveOutcome: outcomes,
	})
}

// VoidMarketsInEvent atomically transitions the listed markets to Voided.
func (repo *PGRepository) VoidMarketsInEvent(ctx context.Context, eventID string, marketIDs []string) (*Event, []*Market, error) {
	if len(marketIDs) == 0 {
		return nil, nil, fmt.Errorf("market_ids is empty: %w", ErrInvalidEvent)
	}
	return repo.transitionMarketsInEvent(ctx, eventID, transitionSpec{
		targetStatus:    StatusVoided,
		voidedMarketIDs: marketIDs,
	})
}

// transitionSpec is the shared input for the resolve/void path. Exactly one
// of resolveOutcome / voidedMarketIDs is populated.
type transitionSpec struct {
	targetStatus    Status
	resolveOutcome  map[string]Outcome
	voidedMarketIDs []string
}

// transitionMarketsInEvent is the shared transactional helper for
// ResolveMarketsInEvent and VoidMarketsInEvent. Each phase is broken out
// into a named helper below so the main function reads as the sequence
// of operations the transaction performs.
func (repo *PGRepository) transitionMarketsInEvent(ctx context.Context, eventID string, spec transitionSpec) (*Event, []*Market, error) {
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("transitioning markets: beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := lockEventForTransition(ctx, tx, eventID); err != nil {
		return nil, nil, err
	}

	siblings, err := lockSiblingMarkets(ctx, tx, eventID)
	if err != nil {
		return nil, nil, err
	}

	pendingIDs, err := validateTransitionTargets(spec, siblings, eventID)
	if err != nil {
		return nil, nil, err
	}

	if err := applyTransitionToMarkets(ctx, tx, spec, pendingIDs, siblings); err != nil {
		return nil, nil, err
	}

	if err := maybeFlipEventStatus(ctx, tx, siblings, eventID); err != nil {
		return nil, nil, err
	}

	updatedEvent, updatedMarkets, err := reReadEventAndMarkets(ctx, tx, eventID)
	if err != nil {
		return nil, nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, nil, fmt.Errorf("transitioning markets: committing: %w", err)
	}
	return updatedEvent, updatedMarkets, nil
}

// lockEventForTransition acquires a row-level lock on the event row,
// blocking concurrent transitions on the same event. Returns ErrNotFound
// if no row matches eventID.
func lockEventForTransition(ctx context.Context, tx pgx.Tx, eventID string) error {
	var existing string
	err := tx.QueryRow(ctx, `SELECT id FROM events WHERE id = $1 FOR UPDATE`, eventID).Scan(&existing)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("event %s: %w", eventID, ErrNotFound)
		}
		return fmt.Errorf("locking event: %w", err)
	}
	return nil
}

// siblingMarket is the locked in-transaction view of a market used by the
// transition phases: current status, plus the stored outcome so a resolve
// retry can be recognized as idempotent (same outcome) vs conflicting.
type siblingMarket struct {
	status  Status
	outcome *Outcome
}

// lockSiblingMarkets row-locks every market belonging to the event and
// returns a map of marketID → current status + outcome. The lock holds
// for the lifetime of the surrounding transaction.
func lockSiblingMarkets(ctx context.Context, tx pgx.Tx, eventID string) (map[string]siblingMarket, error) {
	rows, err := tx.Query(ctx,
		`SELECT id, status, outcome FROM markets WHERE event_id = $1 FOR UPDATE`, eventID,
	)
	if err != nil {
		return nil, fmt.Errorf("locking sibling markets: %w", err)
	}
	defer rows.Close()
	siblings := make(map[string]siblingMarket)
	for rows.Next() {
		var id string
		var sibling siblingMarket
		if err := rows.Scan(&id, &sibling.status, &sibling.outcome); err != nil {
			return nil, fmt.Errorf("scanning sibling market: %w", err)
		}
		siblings[id] = sibling
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating sibling markets: %w", err)
	}
	return siblings, nil
}

// validateTransitionTargets returns the market IDs the transition still
// needs to touch, after verifying each target belongs to the event and is
// in a state that can transition to spec.targetStatus.
//
// Idempotency: a target already in the target state is skipped, not
// rejected — for resolve, only when its stored outcome matches the
// declared one (a different outcome is a genuine conflict). This lets a
// caller retry the whole operation after a failed downstream publish.
// Returns ErrInvalidMarket for membership failures and an
// ErrInvalidTransition-wrapping error for state-transition failures.
func validateTransitionTargets(spec transitionSpec, siblings map[string]siblingMarket, eventID string) ([]string, error) {
	var targetIDs []string
	if spec.resolveOutcome != nil {
		targetIDs = make([]string, 0, len(spec.resolveOutcome))
		for marketID := range spec.resolveOutcome {
			targetIDs = append(targetIDs, marketID)
		}
	} else {
		targetIDs = spec.voidedMarketIDs
	}
	var pending []string
	for _, marketID := range targetIDs {
		sibling, ok := siblings[marketID]
		if !ok {
			return nil, fmt.Errorf("market %s does not belong to event %s: %w", marketID, eventID, ErrInvalidMarket)
		}
		if sibling.status == spec.targetStatus {
			if spec.resolveOutcome != nil {
				declared := spec.resolveOutcome[marketID]
				if sibling.outcome == nil || *sibling.outcome != declared {
					return nil, fmt.Errorf(
						"market %s already resolved with a different outcome: %w",
						marketID, ErrInvalidTransition)
				}
			}
			continue
		}
		if err := ValidateStatusTransition(sibling.status, spec.targetStatus); err != nil {
			return nil, err
		}
		pending = append(pending, marketID)
	}
	return pending, nil
}

// applyTransitionToMarkets writes the new status (and outcome, when
// resolving) to each pending market. Targets already in the target state
// were filtered out by validateTransitionTargets. Mutates siblings in
// place to reflect the post-update statuses — the next phase reads it to
// decide whether to auto-flip the event row.
func applyTransitionToMarkets(ctx context.Context, tx pgx.Tx, spec transitionSpec, pendingIDs []string, siblings map[string]siblingMarket) error {
	if spec.resolveOutcome != nil {
		// Per-market UPDATE: outcomes vary, so a single ANY() statement
		// won't carry them. The market set is tiny (< 50 in practice).
		for _, marketID := range pendingIDs {
			outcome := spec.resolveOutcome[marketID]
			if _, err := tx.Exec(ctx,
				`UPDATE markets
				 SET    status = $1,
				        outcome = $2,
				        updated_at = now()
				 WHERE  id = $3`,
				spec.targetStatus, outcome, marketID,
			); err != nil {
				return fmt.Errorf("updating market %s: %w", marketID, err)
			}
			siblings[marketID] = siblingMarket{status: spec.targetStatus, outcome: &outcome}
		}
		return nil
	}
	if len(pendingIDs) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx,
		`UPDATE markets
		 SET    status = $1,
		        updated_at = now()
		 WHERE  id = ANY($2)`,
		spec.targetStatus, pendingIDs,
	); err != nil {
		return fmt.Errorf("voiding markets: %w", err)
	}
	for _, marketID := range pendingIDs {
		siblings[marketID] = siblingMarket{status: spec.targetStatus}
	}
	return nil
}

// maybeFlipEventStatus auto-flips the event row to a terminal status
// when every sibling market is itself terminal. Resolved wins over
// Voided in mixed-terminal events (any sibling Resolved ⇒ event
// Resolved; all-Voided ⇒ event Voided). No-op otherwise.
func maybeFlipEventStatus(ctx context.Context, tx pgx.Tx, siblings map[string]siblingMarket, eventID string) error {
	allTerminal := true
	hasResolved := false
	for _, sibling := range siblings {
		if !sibling.status.IsTerminal() {
			allTerminal = false
			break
		}
		if sibling.status == StatusResolved {
			hasResolved = true
		}
	}
	if !allTerminal {
		return nil
	}
	nextEventStatus := StatusVoided
	if hasResolved {
		nextEventStatus = StatusResolved
	}
	if _, err := tx.Exec(ctx,
		`UPDATE events SET status = $1, updated_at = now() WHERE id = $2`,
		nextEventStatus, eventID,
	); err != nil {
		return fmt.Errorf("updating event status: %w", err)
	}
	return nil
}

// reReadEventAndMarkets returns the event row and its child markets read
// from inside the open transaction, so the caller sees the just-applied
// state instead of pre-update values cached from earlier phases.
func reReadEventAndMarkets(ctx context.Context, tx pgx.Tx, eventID string) (*Event, []*Market, error) {
	eventRows, err := tx.Query(ctx, `SELECT * FROM events WHERE id = $1`, eventID)
	if err != nil {
		return nil, nil, fmt.Errorf("re-reading event: %w", err)
	}
	updatedEvent, err := pgx.CollectOneRow(eventRows, pgx.RowToAddrOfStructByName[Event])
	if err != nil {
		return nil, nil, fmt.Errorf("scanning updated event: %w", err)
	}
	marketRows, err := tx.Query(ctx,
		`SELECT * FROM markets WHERE event_id = $1 ORDER BY created_at`, eventID,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("re-reading markets: %w", err)
	}
	updatedMarkets, err := pgx.CollectRows(marketRows, pgx.RowToAddrOfStructByName[Market])
	if err != nil {
		return nil, nil, fmt.Errorf("scanning updated markets: %w", err)
	}
	return updatedEvent, updatedMarkets, nil
}

// statusSlice converts Status values to int16 for pgx ANY() binding.
func statusSlice(statuses []Status) []int16 {
	out := make([]int16, len(statuses))
	for idx, status := range statuses {
		out[idx] = int16(status)
	}
	return out
}
