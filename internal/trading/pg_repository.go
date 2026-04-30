package trading

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

// SaveOrder persists a new order. Returns ErrDuplicateOrder if the signature
// hash already exists.
func (repo *PGRepository) SaveOrder(ctx context.Context, order *Order) error {
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("saving order: beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var exists bool
	err = tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM orders WHERE signature_hash = $1)`,
		order.SignatureHash,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("saving order: checking idempotency: %w", err)
	}
	if exists {
		return ErrDuplicateOrder
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO orders (
			maker, token_id, maker_amount, taker_amount, salt,
			expiration, nonce, fee_rate_bps, side, signature_type,
			signature, status, order_type, market_id, signature_hash
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING id, created_at, updated_at`,
		order.Maker, order.TokenID, order.MakerAmount, order.TakerAmount,
		order.Salt, order.Expiration, order.Nonce, order.FeeRateBps,
		order.Side, order.SignatureType, order.Signature,
		order.Status, order.OrderType, order.MarketID, order.SignatureHash,
	)
	if err != nil {
		return fmt.Errorf("saving order: inserting row: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("saving order: committing: %w", err)
	}
	return nil
}

// GetOrder retrieves an order by ID. Returns ErrNotFound if not found.
func (repo *PGRepository) GetOrder(ctx context.Context, id string) (*Order, error) {
	rows, err := repo.pool.Query(ctx, `SELECT * FROM orders WHERE id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("getting order %s: %w", id, err)
	}
	order, err := pgx.CollectOneRow(rows, pgx.RowToAddrOfStructByName[Order])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("getting order %s: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("getting order %s: %w", id, err)
	}
	return order, nil
}

// ListOrdersByUser returns orders for a user, optionally filtered by statuses.
func (repo *PGRepository) ListOrdersByUser(ctx context.Context, userAddress string, statuses []OrderStatus) ([]*Order, error) {
	var rows pgx.Rows
	var err error

	if len(statuses) == 0 {
		rows, err = repo.pool.Query(ctx,
			`SELECT * FROM orders WHERE maker = $1 ORDER BY created_at DESC`,
			userAddress,
		)
	} else {
		rows, err = repo.pool.Query(ctx,
			`SELECT * FROM orders WHERE maker = $1 AND status = ANY($2) ORDER BY created_at DESC`,
			userAddress, statusSlice(statuses),
		)
	}
	if err != nil {
		return nil, fmt.Errorf("listing orders for user %s: %w", userAddress, err)
	}
	orders, err := pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[Order])
	if err != nil {
		return nil, fmt.Errorf("scanning orders for user %s: %w", userAddress, err)
	}
	return orders, nil
}

// ListOrdersByMarket returns orders for a market, optionally filtered by statuses.
func (repo *PGRepository) ListOrdersByMarket(ctx context.Context, marketID string, statuses []OrderStatus) ([]*Order, error) {
	var rows pgx.Rows
	var err error

	if len(statuses) == 0 {
		rows, err = repo.pool.Query(ctx,
			`SELECT * FROM orders WHERE market_id = $1 ORDER BY created_at DESC`,
			marketID,
		)
	} else {
		rows, err = repo.pool.Query(ctx,
			`SELECT * FROM orders WHERE market_id = $1 AND status = ANY($2) ORDER BY created_at DESC`,
			marketID, statusSlice(statuses),
		)
	}
	if err != nil {
		return nil, fmt.Errorf("listing orders for market %s: %w", marketID, err)
	}
	orders, err := pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[Order])
	if err != nil {
		return nil, fmt.Errorf("scanning orders for market %s: %w", marketID, err)
	}
	return orders, nil
}

// UpdateOrderStatus changes the status of an order. Returns ErrNotFound if the
// order does not exist.
func (repo *PGRepository) UpdateOrderStatus(ctx context.Context, id string, status OrderStatus) error {
	tag, err := repo.pool.Exec(ctx,
		`UPDATE orders SET status = $1, updated_at = now() WHERE id = $2`,
		status, id,
	)
	if err != nil {
		return fmt.Errorf("updating order %s status: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("updating order %s status: %w", id, ErrNotFound)
	}
	return nil
}

// SaveTrade persists a new trade. Returns ErrDuplicateTrade if the match ID
// already exists.
func (repo *PGRepository) SaveTrade(ctx context.Context, trade *Trade) error {
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("saving trade: beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var exists bool
	err = tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM trades WHERE match_id = $1)`,
		trade.MatchID,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("saving trade: checking idempotency: %w", err)
	}
	if exists {
		return ErrDuplicateTrade
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO trades (
			match_id, maker_order_id, taker_order_id, maker_address,
			taker_address, market_id, price, size, fee
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		trade.MatchID, trade.MakerOrderID, trade.TakerOrderID,
		trade.MakerAddress, trade.TakerAddress, trade.MarketID,
		trade.Price, trade.Size, trade.Fee,
	)
	if err != nil {
		return fmt.Errorf("saving trade: inserting row: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("saving trade: committing: %w", err)
	}
	return nil
}

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

// statusSlice converts OrderStatus values to int16 for pgx ANY() binding.
func statusSlice(statuses []OrderStatus) []int16 {
	out := make([]int16, len(statuses))
	for idx, status := range statuses {
		out[idx] = int16(status)
	}
	return out
}
