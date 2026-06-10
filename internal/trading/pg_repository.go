package trading

import (
	"github.com/jackc/pgx/v5/pgxpool"
)

// PGRepository implements Repository using PostgreSQL via pgx. Methods are
// split per resource: orders in pg_repository_orders.go, trades in
// pg_repository_trades.go, balances in pg_repository_balances.go.
type PGRepository struct {
	pool *pgxpool.Pool
}

// NewPGRepository creates a new PostgreSQL-backed repository.
func NewPGRepository(pool *pgxpool.Pool) *PGRepository {
	return &PGRepository{pool: pool}
}
