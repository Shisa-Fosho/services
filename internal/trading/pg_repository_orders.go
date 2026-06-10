package trading

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

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

// statusSlice converts OrderStatus values to int16 for pgx ANY() binding.
func statusSlice(statuses []OrderStatus) []int16 {
	out := make([]int16, len(statuses))
	for idx, status := range statuses {
		out[idx] = int16(status)
	}
	return out
}
