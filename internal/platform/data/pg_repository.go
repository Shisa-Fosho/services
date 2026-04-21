package data

import (
	"context"
	"errors"
	"fmt"

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

// CreateUser persists a new user. Validates input via ValidateUser before
// persisting. Returns ErrInvalidUser for shape violations, ErrDuplicateUser
// if the address, username, or email already exists.
func (repo *PGRepository) CreateUser(ctx context.Context, user *User) error {
	if err := ValidateUser(user); err != nil {
		return fmt.Errorf("creating user: %w", err)
	}
	_, err := repo.pool.Exec(ctx,
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
func (repo *PGRepository) GetUserByAddress(ctx context.Context, address string) (*User, error) {
	rows, err := repo.pool.Query(ctx, `SELECT * FROM users WHERE address = $1`, address)
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
func (repo *PGRepository) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	rows, err := repo.pool.Query(ctx, `SELECT * FROM users WHERE email = $1`, email)
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
func (repo *PGRepository) UpsertPosition(ctx context.Context, pos *Position) error {
	if err := ValidatePosition(pos); err != nil {
		return fmt.Errorf("upserting position: %w", err)
	}
	_, err := repo.pool.Exec(ctx,
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
func (repo *PGRepository) GetPositionsByUser(ctx context.Context, userAddress string) ([]*Position, error) {
	rows, err := repo.pool.Query(ctx,
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
func (repo *PGRepository) GetPosition(ctx context.Context, userAddress string, marketID string, side Side) (*Position, error) {
	rows, err := repo.pool.Query(ctx,
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
func (repo *PGRepository) StoreRefreshToken(ctx context.Context, token *RefreshToken) error {
	_, err := repo.pool.Exec(ctx,
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
func (repo *PGRepository) GetRefreshToken(ctx context.Context, id string) (*RefreshToken, error) {
	rows, err := repo.pool.Query(ctx,
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
func (repo *PGRepository) RevokeRefreshToken(ctx context.Context, id string) error {
	tag, err := repo.pool.Exec(ctx,
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
func (repo *PGRepository) RevokeAllRefreshTokens(ctx context.Context, userAddress string) error {
	_, err := repo.pool.Exec(ctx,
		`UPDATE refresh_tokens SET revoked = true
		 WHERE user_address = $1 AND revoked = false`, userAddress,
	)
	if err != nil {
		return fmt.Errorf("revoking all refresh tokens for %s: %w", userAddress, err)
	}
	return nil
}

// IsAdminWallet reports whether the given address appears in admin_wallets.
// The address is compared against the stored (lowercase) form exactly —
// callers are expected to normalize with strings.ToLower beforehand.
func (repo *PGRepository) IsAdminWallet(ctx context.Context, address string) (bool, error) {
	var exists bool
	err := repo.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM admin_wallets WHERE address = $1)`, address,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("checking admin wallet %s: %w", address, err)
	}
	return exists, nil
}
