package data

import "context"

// Repository defines the persistence interface for the data domain.
// Implementations must be safe for concurrent use.
type Repository interface {
	// CreateUser persists a new user. Returns ErrDuplicateUser if the address
	// or username already exists.
	CreateUser(ctx context.Context, user *User) error

	// GetUserByAddress retrieves a user by Ethereum address.
	// Returns ErrNotFound if not found.
	GetUserByAddress(ctx context.Context, address string) (*User, error)

	// GetUserByEmail retrieves a user by email address.
	// Returns ErrNotFound if not found.
	GetUserByEmail(ctx context.Context, email string) (*User, error)

	// UpsertPosition creates or updates a position for a user in a market.
	// The composite key is (user_address, market_id, side).
	UpsertPosition(ctx context.Context, pos *Position) error

	// GetPositionsByUser returns all positions for a given user address.
	GetPositionsByUser(ctx context.Context, userAddress string) ([]*Position, error)

	// GetPosition retrieves a single position by its composite key.
	// Returns ErrNotFound if not found.
	GetPosition(ctx context.Context, userAddress string, marketID string, side Side) (*Position, error)
}
