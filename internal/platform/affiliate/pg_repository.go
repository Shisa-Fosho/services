package affiliate

import (
	"context"
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

// CreateReferral persists a new referral. Validates input via ValidateReferral
// before persisting. Also checks for circular referrals (B→A already exists
// when inserting A→B) inside the same transaction.
func (repo *PGRepository) CreateReferral(ctx context.Context, ref *Referral) error {
	if err := ValidateReferral(ref); err != nil {
		return fmt.Errorf("creating referral: %w", err)
	}
	tx, err := repo.pool.Begin(ctx)
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

// RecordEarning persists a new affiliate earning. Validates input via
// ValidateEarning before persisting. Returns ErrDuplicateEarning if the trade
// ID already exists.
func (repo *PGRepository) RecordEarning(ctx context.Context, earning *Earning) error {
	if err := ValidateEarning(earning); err != nil {
		return fmt.Errorf("recording earning: %w", err)
	}
	_, err := repo.pool.Exec(ctx,
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
func (repo *PGRepository) GetEarningsByReferrer(ctx context.Context, referrerAddress string) ([]*Earning, error) {
	rows, err := repo.pool.Query(ctx,
		`SELECT * FROM affiliate_earnings WHERE referrer_address = $1 ORDER BY created_at DESC`,
		referrerAddress,
	)
	if err != nil {
		return nil, fmt.Errorf("listing earnings for %s: %w", referrerAddress, err)
	}
	earnings, err := pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[Earning])
	if err != nil {
		return nil, fmt.Errorf("scanning earnings for %s: %w", referrerAddress, err)
	}
	return earnings, nil
}

// GetClaimableBalance returns the aggregate claimable balance for a referrer.
// Returns a zero-value balance if no earnings exist.
func (repo *PGRepository) GetClaimableBalance(ctx context.Context, referrerAddress string) (*ClaimableBalance, error) {
	var total int64
	err := repo.pool.QueryRow(ctx,
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
