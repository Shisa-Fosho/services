package data

import "context"

// Repository defines the persistence interface for the data domain.
// Implementations must be safe for concurrent use.
type Repository interface {
	// CreateUser persists a new user. Validates input via ValidateUser
	// before persisting. Returns ErrInvalidUser for shape violations,
	// ErrDuplicateUser if the address or username already exists.
	CreateUser(ctx context.Context, user *User) error

	// GetUserByAddress retrieves a user by Ethereum address.
	// Returns ErrNotFound if not found.
	GetUserByAddress(ctx context.Context, address string) (*User, error)

	// GetUserByEmail retrieves a user by email address.
	// Returns ErrNotFound if not found.
	GetUserByEmail(ctx context.Context, email string) (*User, error)

	// UpsertPosition creates or updates a position for a user in a market.
	// The composite key is (user_address, market_id, side).
	// Validates input via ValidatePosition before persisting.
	UpsertPosition(ctx context.Context, pos *Position) error

	// GetPositionsByUser returns all positions for a given user address.
	GetPositionsByUser(ctx context.Context, userAddress string) ([]*Position, error)

	// GetPosition retrieves a single position by its composite key.
	// Returns ErrNotFound if not found.
	GetPosition(ctx context.Context, userAddress string, marketID string, side Side) (*Position, error)

	// StoreRefreshToken persists a new refresh token.
	StoreRefreshToken(ctx context.Context, token *RefreshToken) error

	// GetRefreshToken retrieves a refresh token by ID.
	// Returns ErrNotFound if not found.
	GetRefreshToken(ctx context.Context, id string) (*RefreshToken, error)

	// RevokeRefreshToken marks a refresh token as revoked.
	// Returns ErrNotFound if the token does not exist.
	RevokeRefreshToken(ctx context.Context, id string) error

	// RevokeAllRefreshTokens revokes all active refresh tokens for a user.
	RevokeAllRefreshTokens(ctx context.Context, userAddress string) error

	// UpsertAPIKey creates or updates an API key. On conflict (same key_hash),
	// updates expires_at and hmac_secret_encrypted (idempotent re-derivation).
	// Validates input via ValidateAPIKey before persisting.
	UpsertAPIKey(ctx context.Context, key *APIKey) error

	// GetAPIKeyByHash retrieves a single non-revoked, non-expired API key by its hash.
	// Returns ErrNotFound if the key does not exist, is revoked, or is expired.
	GetAPIKeyByHash(ctx context.Context, keyHash string) (*APIKey, error)

	// GetAPIKeysByUser returns all non-revoked, non-expired API keys for a user.
	GetAPIKeysByUser(ctx context.Context, userAddress string) ([]*APIKey, error)

	// RevokeAPIKey marks an API key as revoked by its key_hash.
	// Returns ErrNotFound if the key does not exist or does not belong to the user.
	RevokeAPIKey(ctx context.Context, keyHash string, userAddress string) error
}
