package trading

import (
	"context"
	"fmt"
)

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
