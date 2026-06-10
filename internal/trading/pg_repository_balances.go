package trading

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/Shisa-Fosho/services/internal/shared/postgres"
)

// GetBalance retrieves the balance for a user. Returns a zero-value Balance
// if the user has no row.
func (repo *PGRepository) GetBalance(ctx context.Context, userAddress string) (*Balance, error) {
	balance := &Balance{UserAddress: userAddress}
	err := repo.pool.QueryRow(ctx,
		`SELECT available, reserved, updated_at
		FROM balances WHERE user_address = $1`, userAddress,
	).Scan(&balance.Available, &balance.Reserved, &balance.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return &Balance{UserAddress: userAddress}, nil
		}
		return nil, fmt.Errorf("getting balance for %s: %w", userAddress, err)
	}
	return balance, nil
}

// ReserveBalance atomically moves funds from available to reserved.
func (repo *PGRepository) ReserveBalance(ctx context.Context, userAddress string, amount int64) error {
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("reserving balance: beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var available int64
	err = tx.QueryRow(ctx,
		`SELECT available FROM balances WHERE user_address = $1 FOR UPDATE`,
		userAddress,
	).Scan(&available)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("reserving balance for %s: %w", userAddress, ErrInsufficientFunds)
		}
		return fmt.Errorf("reserving balance: reading balance: %w", err)
	}

	if available < amount {
		return fmt.Errorf("reserving balance for %s (available=%d, requested=%d): %w",
			userAddress, available, amount, ErrInsufficientFunds)
	}

	_, err = tx.Exec(ctx,
		`UPDATE balances SET available = available - $1, reserved = reserved + $1,
		 updated_at = now() WHERE user_address = $2`,
		amount, userAddress,
	)
	if err != nil {
		return fmt.Errorf("reserving balance: updating: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("reserving balance: committing: %w", err)
	}
	return nil
}

// ReleaseBalance atomically moves funds from reserved back to available.
func (repo *PGRepository) ReleaseBalance(ctx context.Context, userAddress string, amount int64) error {
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("releasing balance: beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx,
		`UPDATE balances SET reserved = reserved - $1, available = available + $1,
		 updated_at = now() WHERE user_address = $2`,
		amount, userAddress,
	)
	if err != nil {
		if postgres.IsCheckViolation(err) {
			return fmt.Errorf("releasing balance for %s: %w", userAddress, ErrInsufficientFunds)
		}
		return fmt.Errorf("releasing balance: updating: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("releasing balance: committing: %w", err)
	}
	return nil
}

// DeductReserved atomically removes funds from reserved after a trade fills.
func (repo *PGRepository) DeductReserved(ctx context.Context, userAddress string, amount int64) error {
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("deducting reserved: beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx,
		`UPDATE balances SET reserved = reserved - $1,
		 updated_at = now() WHERE user_address = $2`,
		amount, userAddress,
	)
	if err != nil {
		if postgres.IsCheckViolation(err) {
			return fmt.Errorf("deducting reserved for %s: %w", userAddress, ErrInsufficientFunds)
		}
		return fmt.Errorf("deducting reserved: updating: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("deducting reserved: committing: %w", err)
	}
	return nil
}

// CreditAvailable adds funds to a user's available balance.
// Creates the balance row if it does not exist (UPSERT).
func (repo *PGRepository) CreditAvailable(ctx context.Context, userAddress string, amount int64) error {
	_, err := repo.pool.Exec(ctx,
		`INSERT INTO balances (user_address, available, reserved, updated_at)
		 VALUES ($1, $2, 0, now())
		 ON CONFLICT (user_address)
		 DO UPDATE SET available = balances.available + $2, updated_at = now()`,
		userAddress, amount,
	)
	if err != nil {
		return fmt.Errorf("crediting available for %s: %w", userAddress, err)
	}
	return nil
}
