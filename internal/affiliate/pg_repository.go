package affiliate

import (
	"context"
	"fmt"

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

// CreateReferral persists a new referral. Checks for circular referrals
// (B→A already exists when inserting A→B) inside the same transaction.
func (r *PGRepository) CreateReferral(ctx context.Context, ref *Referral) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("creating referral: beginning transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var exists bool
	err = tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM referrals WHERE referrer_address = $1 AND referred_address = $2)`,
		ref.ReferredAddress, ref.ReferrerAddress,
	).Scan(&exists)
	if err != nil {
		return fmt.Errorf("creating referral: checking circular: %w", err)
	}
	if exists {
		return fmt.Errorf("creating referral %s→%s: %w",
			ref.ReferrerAddress, ref.ReferredAddress, ErrCircularReferral)
	}

	_, err = tx.Exec(ctx,
		`INSERT INTO referrals (referrer_address, referred_address)
		 VALUES ($1, $2)`,
		ref.ReferrerAddress, ref.ReferredAddress,
	)
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			return fmt.Errorf("creating referral %s→%s: %w",
				ref.ReferrerAddress, ref.ReferredAddress, ErrDuplicateReferral)
		}
		if postgres.IsCheckViolation(err) {
			return fmt.Errorf("creating referral %s→%s: %w",
				ref.ReferrerAddress, ref.ReferredAddress, ErrSelfReferral)
		}
		return fmt.Errorf("creating referral: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("creating referral: committing: %w", err)
	}
	return nil
}

// RecordEarning persists a new affiliate earning. Returns ErrDuplicateEarning
// if the trade ID already exists.
func (r *PGRepository) RecordEarning(ctx context.Context, earning *Earning) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO affiliate_earnings (referrer_address, trade_id, fee_amount, referrer_cut)
		 VALUES ($1, $2, $3, $4)`,
		earning.ReferrerAddress, earning.TradeID,
		earning.FeeAmount, earning.ReferrerCut,
	)
	if err != nil {
		if postgres.IsUniqueViolation(err) {
			return fmt.Errorf("recording earning for trade %s: %w",
				earning.TradeID, ErrDuplicateEarning)
		}
		return fmt.Errorf("recording earning: %w", err)
	}
	return nil
}

// GetEarningsByReferrer returns all earnings for a referrer.
func (r *PGRepository) GetEarningsByReferrer(ctx context.Context, referrerAddress string) ([]*Earning, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, referrer_address, trade_id, fee_amount, referrer_cut, created_at
		FROM affiliate_earnings WHERE referrer_address = $1
		ORDER BY created_at DESC`, referrerAddress,
	)
	if err != nil {
		return nil, fmt.Errorf("listing earnings for %s: %w", referrerAddress, err)
	}
	defer rows.Close()

	var earnings []*Earning
	for rows.Next() {
		e := &Earning{}
		if err := rows.Scan(
			&e.ID, &e.ReferrerAddress, &e.TradeID,
			&e.FeeAmount, &e.ReferrerCut, &e.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scanning earning row: %w", err)
		}
		earnings = append(earnings, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating earning rows: %w", err)
	}
	return earnings, nil
}

// GetClaimableBalance returns the aggregate claimable balance for a referrer.
// Returns a zero-value balance if no earnings exist.
func (r *PGRepository) GetClaimableBalance(ctx context.Context, referrerAddress string) (*ClaimableBalance, error) {
	var total int64
	err := r.pool.QueryRow(ctx,
		`SELECT COALESCE(SUM(referrer_cut), 0) FROM affiliate_earnings
		 WHERE referrer_address = $1`, referrerAddress,
	).Scan(&total)
	if err != nil {
		return nil, fmt.Errorf("getting claimable balance for %s: %w", referrerAddress, err)
	}
	return &ClaimableBalance{
		ReferrerAddress: referrerAddress,
		TotalEarned:     total,
		Claimable:       total,
	}, nil
}
