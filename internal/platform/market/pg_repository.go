package market

import (
	"context"
	"errors"
	"fmt"
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

// CreateEvent persists a new event. Validates input via ValidateEvent before
// persisting. Returns ErrInvalidEvent for shape violations, ErrDuplicateSlug
// if the slug already exists.
func (repo *PGRepository) CreateEvent(ctx context.Context, event *Event) error {
	if err := ValidateEvent(event, time.Now()); err != nil {
		return fmt.Errorf("creating event: %w", err)
	}
	_, err := repo.pool.Exec(ctx,
		`INSERT INTO events (
			slug, title, description, category_id, event_type,
			resolution_config, status, end_date, featured, featured_sort_order
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		event.Slug, event.Title, event.Description, event.CategoryID,
		event.EventType, event.ResolutionConfig, event.Status,
		event.EndDate, event.Featured, event.FeaturedSortOrder,
	)
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			return fmt.Errorf("creating event %q: %w", event.Slug, ErrDuplicateSlug)
		}
		return fmt.Errorf("creating event: %w", err)
	}
	return nil
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

// CreateMarket persists a new market. Validates input via ValidateMarket
// before persisting. Returns ErrInvalidMarket for shape violations,
// ErrDuplicateSlug if the slug already exists.
func (repo *PGRepository) CreateMarket(ctx context.Context, market *Market) error {
	if err := ValidateMarket(market); err != nil {
		return fmt.Errorf("creating market: %w", err)
	}
	_, err := repo.pool.Exec(ctx,
		`INSERT INTO markets (
			slug, event_id, question, outcome_yes_label, outcome_no_label,
			token_id_yes, token_id_no, condition_id, status, outcome,
			price_yes, price_no, volume, open_interest
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		market.Slug, market.EventID, market.Question,
		market.OutcomeYesLabel, market.OutcomeNoLabel,
		market.TokenIDYes, market.TokenIDNo, market.ConditionID,
		market.Status, market.Outcome, market.PriceYes, market.PriceNo,
		market.Volume, market.OpenInterest,
	)
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			return fmt.Errorf("creating market %q: %w", market.Slug, ErrDuplicateSlug)
		}
		return fmt.Errorf("creating market: %w", err)
	}
	return nil
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

// GetFeeRate returns the stored rate for a market, or ErrNotFound if absent.
func (repo *PGRepository) GetFeeRate(ctx context.Context, marketID string) (*FeeRate, error) {
	rate := &FeeRate{}
	err := repo.pool.QueryRow(ctx,
		`SELECT market_id, fee_rate_bps, updated_at FROM market_fee_rates WHERE market_id = $1`,
		marketID,
	).Scan(&rate.MarketID, &rate.FeeRateBps, &rate.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("getting fee rate for market %s: %w", marketID, ErrNotFound)
		}
		return nil, fmt.Errorf("getting fee rate for market %s: %w", marketID, err)
	}
	return rate, nil
}

// UpsertFeeRate validates and writes a fee rate for a market. A missing
// market_id surfaces as ErrNotFound via the FK constraint.
func (repo *PGRepository) UpsertFeeRate(ctx context.Context, rate *FeeRate) (*FeeRate, error) {
	if err := ValidateFeeRate(rate); err != nil {
		return nil, fmt.Errorf("upserting fee rate: %w", err)
	}
	result := &FeeRate{}
	err := repo.pool.QueryRow(ctx,
		`INSERT INTO market_fee_rates (market_id, fee_rate_bps, updated_at)
		 VALUES ($1, $2, now())
		 ON CONFLICT (market_id) DO UPDATE SET
		     fee_rate_bps = EXCLUDED.fee_rate_bps,
		     updated_at = now()
		 RETURNING market_id, fee_rate_bps, updated_at`,
		rate.MarketID, rate.FeeRateBps,
	).Scan(&result.MarketID, &result.FeeRateBps, &result.UpdatedAt)
	if err != nil {
		if postgres.IsForeignKeyViolation(err) {
			return nil, fmt.Errorf("upserting fee rate for market %s: %w", rate.MarketID, ErrNotFound)
		}
		return nil, fmt.Errorf("upserting fee rate for market %s: %w", rate.MarketID, err)
	}
	return result, nil
}

// statusSlice converts Status values to int16 for pgx ANY() binding.
func statusSlice(statuses []Status) []int16 {
	out := make([]int16, len(statuses))
	for idx, status := range statuses {
		out[idx] = int16(status)
	}
	return out
}
