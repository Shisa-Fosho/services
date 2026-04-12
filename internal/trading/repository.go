package trading

import "context"

// Repository defines the persistence interface for the trading domain.
// Implementations must be safe for concurrent use.
type Repository interface {
	// SaveOrder persists a new order. Returns ErrDuplicateOrder if the
	// signature hash already exists (idempotency).
	SaveOrder(ctx context.Context, order *Order) error

	// GetOrder retrieves an order by ID. Returns ErrNotFound if not found.
	GetOrder(ctx context.Context, id string) (*Order, error)

	// ListOrdersByUser returns orders for the given user address filtered by
	// statuses. If statuses is empty, all orders are returned.
	ListOrdersByUser(ctx context.Context, userAddress string, statuses []OrderStatus) ([]*Order, error)

	// ListOrdersByMarket returns orders for the given market filtered by
	// statuses. If statuses is empty, all orders are returned.
	ListOrdersByMarket(ctx context.Context, marketID string, statuses []OrderStatus) ([]*Order, error)

	// ListOpenOrders returns all orders with status OPEN or PARTIALLY_FILLED
	// across every market, ordered by created_at ascending. Used by the
	// matching engine to rebuild in-memory books on startup.
	ListOpenOrders(ctx context.Context) ([]*Order, error)

	// UpdateOrderStatus changes the status of an existing order.
	// Returns ErrNotFound if the order does not exist.
	UpdateOrderStatus(ctx context.Context, id string, status OrderStatus) error

	// SaveTrade persists a new trade. Returns ErrDuplicateTrade if the
	// match ID already exists (idempotency).
	SaveTrade(ctx context.Context, trade *Trade) error

	// GetBalance retrieves the balance for a user. Returns a zero-value
	// Balance (not an error) if the user has no balance row.
	GetBalance(ctx context.Context, userAddress string) (*Balance, error)

	// ReserveBalance atomically moves funds from available to reserved.
	// Returns ErrInsufficientFunds if available < amount.
	ReserveBalance(ctx context.Context, userAddress string, amount int64) error

	// ReleaseBalance atomically moves funds from reserved back to available.
	// Used when an order is cancelled.
	ReleaseBalance(ctx context.Context, userAddress string, amount int64) error

	// DeductReserved atomically removes funds from reserved (after a trade fills).
	// Returns ErrInsufficientFunds if reserved < amount.
	DeductReserved(ctx context.Context, userAddress string, amount int64) error

	// CreditAvailable adds funds to a user's available balance.
	// Creates the balance row if it does not exist (UPSERT).
	CreditAvailable(ctx context.Context, userAddress string, amount int64) error
}
