package data

import "context"

// SessionRepository is the platform service's persistence interface for
// session-auth state (users + refresh tokens) and the position read/write
// primitives shared with downstream services.
//
// API-key storage lives entirely in the trading service — see
// internal/trading/auth.APIKeyRepository for derive/list/revoke and
// internal/trading/auth.APIKeyReader for the narrow read interface used by
// the L2 HMAC middleware.
//
// Implementations must be safe for concurrent use.
type SessionRepository interface {
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
}

// AdminRepository is the narrow read interface consumed by the platform
// service's admin authorization middleware. It is deliberately separate from
// SessionRepository so that the middleware's injection surface stays minimal
// and unit tests can supply a tiny fake.
//
// Implementations must be safe for concurrent use.
type AdminRepository interface {
	// IsAdminWallet reports whether the given Ethereum address is present in
	// the admin_wallets table. The caller is expected to normalize the address
	// (e.g., strings.ToLower) before calling — the underlying column stores
	// lowercase form.
	IsAdminWallet(ctx context.Context, address string) (bool, error)
}

// AdminAuditAction describes a single admin HTTP action to be recorded.
type AdminAuditAction struct {
	AdminAddress string
	Method       string
	Path         string
	Status       int
}

// AdminAuditRepository is the narrow write interface consumed by the admin
// audit middleware. A separate interface keeps the middleware's injection
// surface minimal and lets tests use a tiny in-memory fake.
//
// Implementations must be safe for concurrent use.
type AdminAuditRepository interface {
	// RecordAdminAction appends a single audit log entry. Failures should be
	// returned to the caller; the middleware logs and swallows them so the
	// underlying request is unaffected.
	RecordAdminAction(ctx context.Context, action *AdminAuditAction) error
}
