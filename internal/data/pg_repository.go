package data

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGRepository implements Repository using PostgreSQL via pgx.
type PGRepository struct {
	pool *pgxpool.Pool
}

// NewPGRepository creates a new PostgreSQL-backed repository.
func NewPGRepository(pool *pgxpool.Pool) *PGRepository {
	return &PGRepository{pool: pool}
}

// CreateUser persists a new user. Returns ErrDuplicateUser if the address,
// username, or email already exists.
func (r *PGRepository) CreateUser(ctx context.Context, user *User) error {
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
		if isPgUniqueViolation(err) {
			return fmt.Errorf("creating user %s: %w", user.Address, ErrDuplicateUser)
		}
		return fmt.Errorf("creating user: %w", err)
	}
	return nil
}

// GetUserByAddress retrieves a user by Ethereum address.
func (r *PGRepository) GetUserByAddress(ctx context.Context, address string) (*User, error) {
	u := &User{}
	err := r.pool.QueryRow(ctx,
		`SELECT address, username, email, signup_method, safe_address,
			proxy_address, twofa_secret_encrypted, twofa_enabled, created_at
		FROM users WHERE address = $1`, address,
	).Scan(
		&u.Address, &u.Username, &u.Email, &u.SignupMethod,
		&u.SafeAddress, &u.ProxyAddress,
		&u.TwoFASecretEncrypted, &u.TwoFAEnabled, &u.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("getting user %s: %w", address, ErrNotFound)
		}
		return nil, fmt.Errorf("getting user %s: %w", address, err)
	}
	return u, nil
}

// GetUserByEmail retrieves a user by email address.
func (r *PGRepository) GetUserByEmail(ctx context.Context, email string) (*User, error) {
	u := &User{}
	err := r.pool.QueryRow(ctx,
		`SELECT address, username, email, signup_method, safe_address,
			proxy_address, twofa_secret_encrypted, twofa_enabled, created_at
		FROM users WHERE email = $1`, email,
	).Scan(
		&u.Address, &u.Username, &u.Email, &u.SignupMethod,
		&u.SafeAddress, &u.ProxyAddress,
		&u.TwoFASecretEncrypted, &u.TwoFAEnabled, &u.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("getting user by email: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("getting user by email: %w", err)
	}
	return u, nil
}

// UpsertPosition creates or updates a position for a user in a market.
func (r *PGRepository) UpsertPosition(ctx context.Context, pos *Position) error {
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
		`SELECT user_address, market_id, side, size, average_entry_price, realised_pnl
		FROM positions WHERE user_address = $1
		ORDER BY market_id`, userAddress,
	)
	if err != nil {
		return nil, fmt.Errorf("listing positions for user %s: %w", userAddress, err)
	}
	defer rows.Close()

	return scanPositions(rows)
}

// GetPosition retrieves a single position by its composite key.
func (r *PGRepository) GetPosition(ctx context.Context, userAddress string, marketID string, side Side) (*Position, error) {
	p := &Position{}
	err := r.pool.QueryRow(ctx,
		`SELECT user_address, market_id, side, size, average_entry_price, realised_pnl
		FROM positions WHERE user_address = $1 AND market_id = $2 AND side = $3`,
		userAddress, marketID, side,
	).Scan(
		&p.UserAddress, &p.MarketID, &p.Side,
		&p.Size, &p.AverageEntryPrice, &p.RealisedPnL,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("getting position for %s in market %s: %w",
				userAddress, marketID, ErrNotFound)
		}
		return nil, fmt.Errorf("getting position for %s in market %s: %w",
			userAddress, marketID, err)
	}
	return p, nil
}

// scanPositions collects rows into a slice of positions.
func scanPositions(rows pgx.Rows) ([]*Position, error) {
	var positions []*Position
	for rows.Next() {
		p := &Position{}
		err := rows.Scan(
			&p.UserAddress, &p.MarketID, &p.Side,
			&p.Size, &p.AverageEntryPrice, &p.RealisedPnL,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning position row: %w", err)
		}
		positions = append(positions, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating position rows: %w", err)
	}
	return positions, nil
}

// isPgUniqueViolation returns true if the error is a PostgreSQL unique constraint violation.
func isPgUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505" // unique_violation
	}
	return false
}
