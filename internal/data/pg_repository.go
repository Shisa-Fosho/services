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
