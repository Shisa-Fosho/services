package market

import "context"

// Repository defines the persistence interface for the market domain.
// Implementations must be safe for concurrent use.
type Repository interface {
	// CreateCategory persists a new category and populates cat.ID with the
	// generated UUID. Returns ErrDuplicateSlug if the slug already exists.
	CreateCategory(ctx context.Context, cat *Category) error

	// GetCategory retrieves a category by ID. Returns ErrNotFound if not found.
	GetCategory(ctx context.Context, id string) (*Category, error)

	// ListCategories returns all categories ordered by name.
	ListCategories(ctx context.Context) ([]*Category, error)

	// UpdateCategory changes the name and slug of an existing category and
	// returns the updated row. Returns ErrNotFound if no category has the
	// given id, or ErrDuplicateSlug if the new slug collides with another
	// existing category.
	UpdateCategory(ctx context.Context, id, name, slug string) (*Category, error)

	// DeleteCategory removes a category by id. Returns ErrNotFound if no
	// category has the given id.
	DeleteCategory(ctx context.Context, id string) error

	// CreateEvent persists a new event. Validates input via ValidateEvent
	// before persisting. Returns ErrInvalidEvent for shape violations,
	// ErrDuplicateSlug if the slug already exists.
	CreateEvent(ctx context.Context, event *Event) error

	// GetEvent retrieves an event by ID. Returns ErrNotFound if not found.
	GetEvent(ctx context.Context, id string) (*Event, error)

	// GetEventBySlug retrieves an event by slug. Returns ErrNotFound if not found.
	GetEventBySlug(ctx context.Context, slug string) (*Event, error)

	// ListEvents returns events optionally filtered by statuses. If statuses
	// is empty, all events are returned.
	ListEvents(ctx context.Context, statuses []Status) ([]*Event, error)

	// UpdateEvent applies a partial update to an event's editable metadata
	// and returns the resulting row. Returns ErrNotFound if the id doesn't
	// match a row, or ErrInvalidEvent if the update is empty/invalid. Slug,
	// event type, end date, resolution config, and status are not mutable
	// through this path.
	UpdateEvent(ctx context.Context, id string, update *EventUpdate) (*Event, error)

	// CreateMarket persists a new market. Validates input via ValidateMarket
	// before persisting. Returns ErrInvalidMarket for shape violations,
	// ErrDuplicateSlug if the slug already exists.
	CreateMarket(ctx context.Context, market *Market) error

	// GetMarket retrieves a market by ID. Returns ErrNotFound if not found.
	GetMarket(ctx context.Context, id string) (*Market, error)

	// GetMarketBySlug retrieves a market by slug. Returns ErrNotFound if not found.
	GetMarketBySlug(ctx context.Context, slug string) (*Market, error)

	// ListMarkets returns markets optionally filtered by statuses. If statuses
	// is empty, all markets are returned.
	ListMarkets(ctx context.Context, statuses []Status) ([]*Market, error)

	// ListMarketsByEvent returns all markets belonging to an event.
	ListMarketsByEvent(ctx context.Context, eventID string) ([]*Market, error)

	// UpdateMarketMetadata applies a partial update to a market's editable
	// fields (question, outcome labels) and returns the resulting row.
	// Returns ErrNotFound if the id doesn't match a row, or ErrInvalidMarket
	// if the update is empty/invalid. Slug, token IDs, condition ID, prices,
	// and status are not mutable through this path.
	UpdateMarketMetadata(ctx context.Context, id string, update *MarketUpdate) (*Market, error)

	// UpdateStatus changes the status of a market and returns the updated
	// row. Returns ErrInvalidTransition if the transition is not allowed,
	// or ErrNotFound if the market does not exist.
	UpdateStatus(ctx context.Context, id string, status Status) (*Market, error)

	// UpdateMarketPrices updates the current prices, volume, and open interest
	// for a market. Returns ErrNotFound if the market does not exist.
	UpdateMarketPrices(ctx context.Context, id string, priceYes, priceNo, volume, openInterest int64) error

	// UpdateFeeRate validates and writes a market's fee rate onto the
	// markets row, returning the resulting market. Returns ErrInvalidFeeRate
	// for out-of-range bps and ErrNotFound if marketID does not reference
	// an existing market. Callers read the rate back via GetMarket.
	UpdateFeeRate(ctx context.Context, marketID string, bps int) (*Market, error)
}
