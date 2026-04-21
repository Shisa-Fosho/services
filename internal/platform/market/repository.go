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

	// UpdateStatus changes the status of a market. Returns
	// ErrInvalidTransition if the transition is not allowed, or
	// ErrNotFound if the market does not exist.
	UpdateStatus(ctx context.Context, id string, status Status) error

	// UpdateMarketPrices updates the current prices, volume, and open interest
	// for a market. Returns ErrNotFound if the market does not exist.
	UpdateMarketPrices(ctx context.Context, id string, priceYes, priceNo, volume, openInterest int64) error
}
