package data

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Shisa-Fosho/services/internal/platform/postgres"
)

// PGRepository implements Repository using PostgreSQL via pgx.
type PGRepository struct {
	pool *pgxpool.Pool
}

// NewPGRepository creates a new PostgreSQL-backed repository.
func NewPGRepository(pool *pgxpool.Pool) *PGRepository {
	return &PGRepository{pool: pool}
}

// CreateUser persists a new user. Validates input via ValidateUser before
// persisting. Returns ErrInvalidUser for shape violations, ErrDuplicateUser
// if the address, username, or email already exists.
func (r *PGRepository) CreateUser(ctx context.Context, user *User) error {
	if err := ValidateUser(user); err != nil {
		return fmt.Errorf("creating user: %w", err)
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO users (
			address, username, email, signup_method, safe_address,
			proxy_address, twofa_secret_encrypted, twofa_enabled
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		user.Address, user.Username, user.Email, user.SignupMethod,
		user.SafeAddress, user.ProxyAddress,
		user.TwoFASecretEncrypted, user.TwoFAEnabled,
	)
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			return fmt.Errorf("creating user %s: %w", user.Address, ErrDuplicateUser)
		}
		return fmt.Errorf("creating user: %w", err)
	}
	return nil
}

// GetUserByAddress retrieves a user by Ethereum address.
func (r *PGRepository) GetUserByAddress(ctx context.Context, address string) (*User, error) {
	rows, err := r.pool.Query(ctx, `SELECT * FROM users WHERE address = $1`, address)
	if err != nil {
		return nil, fmt.Errorf("getting user %s: %w", address, err)
	}
	user, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[User])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("getting user %s: %w", address, ErrNotFound)
		}
		return nil, fmt.Errorf("getting user %s: %w", address, err)
	}
	return user, nil
}

// GetUserByEmail retrieves a user by email address.
func (r *PGRepository) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	rows, err := r.pool.Query(ctx, `SELECT * FROM users WHERE email = $1`, email)
	if err != nil {
		return nil, fmt.Errorf("getting user by email: %w", err)
	}
	user, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[User])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("getting user by email: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("getting user by email: %w", err)
	}
	return user, nil
}

// UpsertPosition creates or updates a position for a user in a market.
// Validates input via ValidatePosition before persisting.
func (r *PGRepository) UpsertPosition(ctx context.Context, pos *Position) error {
	if err := ValidatePosition(pos); err != nil {
		return fmt.Errorf("upserting position: %w", err)
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO positions (user_address, market_id, side, size, average_entry_price, realised_pnl)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (user_address, market_id, side)
		 DO UPDATE SET
		     size = $4,
		     average_entry_price = $5,
		     realised_pnl = $6`,
		pos.UserAddress, pos.MarketID, pos.Side, pos.Size, pos.AverageEntryPrice, pos.RealisedPnL,
	)
	if err != nil {
		return fmt.Errorf("upserting position for %s in market %s: %w",
			pos.UserAddress, pos.MarketID, err)
	}
	return nil
}

// GetPositionsByUser returns all positions for a given user address.
func (r *PGRepository) GetPositionsByUser(ctx context.Context, userAddress string) ([]*Position, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM positions WHERE user_address = $1 ORDER BY market_id`,
		userAddress,
	)
	if err != nil {
		return nil, fmt.Errorf("listing positions for user %s: %w", userAddress, err)
	}
	positions, err := pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[Position])
	if err != nil {
		return nil, fmt.Errorf("scanning positions for user %s: %w", userAddress, err)
	}
	return positions, nil
}

// GetPosition retrieves a single position by its composite key.
func (r *PGRepository) GetPosition(ctx context.Context, userAddress string, marketID string, side Side) (*Position, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM positions WHERE user_address = $1 AND market_id = $2 AND side = $3`,
		userAddress, marketID, side,
	)
	if err != nil {
		return nil, fmt.Errorf("getting position for %s in market %s: %w",
			userAddress, marketID, err)
	}
	position, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[Position])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("getting position for %s in market %s: %w",
				userAddress, marketID, ErrNotFound)
		}
		return nil, fmt.Errorf("getting position for %s in market %s: %w",
			userAddress, marketID, err)
	}
	return position, nil
}

// StoreRefreshToken persists a new refresh token.
func (r *PGRepository) StoreRefreshToken(ctx context.Context, token *RefreshToken) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO refresh_tokens (id, user_address, expires_at)
		 VALUES ($1, $2, $3)`,
		token.ID, token.UserAddress, token.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("storing refresh token: %w", err)
	}
	return nil
}

// GetRefreshToken retrieves a refresh token by ID.
func (r *PGRepository) GetRefreshToken(ctx context.Context, id string) (*RefreshToken, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM refresh_tokens WHERE id = $1`, id,
	)
	if err != nil {
		return nil, fmt.Errorf("getting refresh token: %w", err)
	}
	token, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[RefreshToken])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("getting refresh token %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("getting refresh token: %w", err)
	}
	return token, nil
}

// RevokeRefreshToken marks a refresh token as revoked.
func (r *PGRepository) RevokeRefreshToken(ctx context.Context, id string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked = true WHERE id = $1`, id,
	)
	if err != nil {
		return fmt.Errorf("revoking refresh token: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("revoking refresh token %s: %w", id, ErrNotFound)
	}
	return nil
}

// RevokeAllRefreshTokens revokes all active refresh tokens for a user.
func (r *PGRepository) RevokeAllRefreshTokens(ctx context.Context, userAddress string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked = true
		 WHERE user_address = $1 AND revoked = false`, userAddress,
	)
	if err != nil {
		return fmt.Errorf("revoking all refresh tokens for %s: %w", userAddress, err)
	}
	return nil
}

// UpsertAPIKey creates or updates an API key. On conflict (same key_hash),
// updates expires_at and hmac_secret_encrypted (idempotent re-derivation).
func (r *PGRepository) UpsertAPIKey(ctx context.Context, key *APIKey) error {
	if err := ValidateAPIKey(key); err != nil {
		return fmt.Errorf("upserting api key: %w", err)
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO api_keys (key_hash, user_address, hmac_secret_encrypted, label, expires_at)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (key_hash)
		 DO UPDATE SET expires_at = $5, hmac_secret_encrypted = $3
		 WHERE api_keys.user_address = $2`,
		key.KeyHash, key.UserAddress, key.HMACSecretEncrypted, key.Label, key.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("upserting api key for %s: %w", key.UserAddress, err)
	}
	return nil
}

// GetAPIKeyByHash retrieves a single non-revoked, non-expired API key by its hash.
func (r *PGRepository) GetAPIKeyByHash(ctx context.Context, keyHash string) (*APIKey, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM api_keys
		 WHERE key_hash = $1 AND revoked = false AND expires_at > now()`,
		keyHash,
	)
	if err != nil {
		return nil, fmt.Errorf("getting api key by hash: %w", err)
	}
	key, err := pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[APIKey])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("scanning api key by hash: %w", err)
	}
	return key, nil
}

// GetAPIKeysByUser returns all non-revoked, non-expired API keys for a user.
func (r *PGRepository) GetAPIKeysByUser(ctx context.Context, userAddress string) ([]*APIKey, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM api_keys
		 WHERE user_address = $1 AND revoked = false AND expires_at > now()
		 ORDER BY created_at DESC`,
		userAddress,
	)
	if err != nil {
		return nil, fmt.Errorf("listing api keys for %s: %w", userAddress, err)
	}
	keys, err := pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[APIKey])
	if err != nil {
		return nil, fmt.Errorf("scanning api keys for %s: %w", userAddress, err)
	}
	return keys, nil
}

// RevokeAPIKey marks an API key as revoked by its key_hash, scoped to user.
func (r *PGRepository) RevokeAPIKey(ctx context.Context, keyHash string, userAddress string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET revoked = true
		 WHERE key_hash = $1 AND user_address = $2 AND revoked = false`,
		keyHash, userAddress,
	)
	if err != nil {
		return fmt.Errorf("revoking api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("revoking api key %s: %w", keyHash, ErrNotFound)
	}
	return nil
}
