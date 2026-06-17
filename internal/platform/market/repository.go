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

	// CreateEventWithMarket inserts an event and its single initial market
	// in one transaction. Events are created with exactly one on-chain-backed
	// market; additional markets are appended afterward via AddMarketToEvent.
	// Returns ErrDuplicateSlug on any unique-constraint violation (event slug,
	// market slug, condition_id, or question_id), and ErrInvalidEvent /
	// ErrInvalidMarket on shape violations. The market is persisted with
	// Status = Active regardless of the value supplied by the caller.
	//
	// Caller-supplied IDs / CreatedAt / UpdatedAt fields are ignored;
	// the database fills them in and the returned values carry the
	// authoritative state.
	CreateEventWithMarket(ctx context.Context, event *Event, market *Market) (*Event, *Market, error)

	// AddMarketToEvent appends one market to an existing non-terminal event in
	// one transaction. The event row is locked before the insert so concurrent
	// lifecycle transitions and append attempts serialize. Returns the event
	// row and the requested market (newly inserted, or the exact existing match
	// for idempotent retry after a failed downstream publish).
	//
	// Returns ErrNotFound if eventID is missing, ErrInvalidTransition if the
	// event is terminal, ErrInvalidMarket for an invalid market, and
	// ErrDuplicateSlug when slug, condition_id, or question_id collides with
	// a different market.
	AddMarketToEvent(ctx context.Context, eventID string, market *Market) (*Event, *Market, error)

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
	// row. Idempotent: a market already in the requested status is
	// returned unchanged (no error), so callers can retry after a failed
	// downstream publish. Returns ErrInvalidTransition if the transition
	// is not allowed, or ErrNotFound if the market does not exist.
	UpdateStatus(ctx context.Context, id string, status Status) (*Market, error)

	// PauseMarkets atomically transitions every market in marketIDs from
	// Active to Paused inside one transaction. Markets are locked in
	// sorted-ID order to avoid deadlocks with concurrent bulk operations.
	//
	// Semantics are all-or-nothing: if any market is missing, returns
	// ErrNotFound; if any market is in a status other than Active or
	// Paused, returns ErrInvalidTransition; in either case no rows are
	// updated. Already-Paused markets are idempotent no-ops, so retrying
	// the same batch after a failed publish succeeds. Returns
	// ErrInvalidMarket if marketIDs is empty. On success, the returned
	// slice carries every requested market (updated or already Paused)
	// in sorted-ID order.
	PauseMarkets(ctx context.Context, marketIDs []string) ([]*Market, error)

	// UpdateMarketPrices updates the current prices, volume, and open interest
	// for a market. Returns ErrNotFound if the market does not exist.
	UpdateMarketPrices(ctx context.Context, id string, priceYes, priceNo, volume, openInterest int64) error

	// UpdateFeeRate validates and writes a market's fee rate onto the
	// markets row, returning the resulting market. Returns ErrInvalidFeeRate
	// for out-of-range bps and ErrNotFound if marketID does not reference
	// an existing market. Callers read the rate back via GetMarket.
	UpdateFeeRate(ctx context.Context, marketID string, bps int) (*Market, error)

	// UpdateTradingConfig writes tick_size/min_size/max_size onto a market
	// and returns the resulting row. Returns ErrInvalidMarket for shape
	// violations and ErrNotFound if the market does not exist.
	UpdateTradingConfig(ctx context.Context, marketID string, tickSize TickSize, minSize int64, maxSize *int64) (*Market, error)

	// ResolveMarketsInEvent atomically transitions every market named in
	// outcomes to Resolved with its supplied outcome. Sibling markets in
	// the event are left untouched. After the update, if every market in
	// the event is in a terminal status (Resolved or Voided), event.status
	// auto-flips to Resolved.
	//
	// Idempotent: a named market already Resolved with the same outcome
	// is a no-op (the call still succeeds and returns it), so callers can
	// retry after a failed downstream publish. Already Resolved with a
	// different outcome returns ErrInvalidTransition.
	//
	// Errors: ErrNotFound for missing event; ErrInvalidEvent if outcomes
	// is empty; ErrInvalidMarket if any key in outcomes doesn't belong to
	// this event; ErrInvalidTransition if any named market is in a status
	// that cannot reach Resolved.
	ResolveMarketsInEvent(ctx context.Context, eventID string, outcomes map[string]Outcome) (*Event, []*Market, error)

	// VoidMarketsInEvent atomically transitions every market in marketIDs
	// to Voided. Siblings are left untouched. After the update, if every
	// market in the event is in a terminal status (Resolved or Voided),
	// event.status auto-flips to either Resolved (if any are Resolved) or
	// Voided (if all are Voided).
	//
	// Error contract mirrors ResolveMarketsInEvent, including idempotency:
	// already-Voided markets are no-ops so the call is retryable.
	//
	// Note: the handler layer rejects this path for NEG_RISK events. The
	// repository doesn't enforce that — callers should branch by event
	// type before invoking.
	VoidMarketsInEvent(ctx context.Context, eventID string, marketIDs []string) (*Event, []*Market, error)
}
