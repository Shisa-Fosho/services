//go:build integration

package affiliate

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connecting to database: %v", err)
	}

	t.Cleanup(func() { pool.Close() })
	return pool
}

func cleanTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `TRUNCATE affiliate_earnings, referrals CASCADE`)
	if err != nil {
		t.Fatalf("cleaning tables: %v", err)
	}
}

// insertTestTrade creates a trade row needed for affiliate_earnings FK.
func insertTestTrade(t *testing.T, pool *pgxpool.Pool, matchID string) string {
	t.Helper()
	ctx := context.Background()

	// Ensure prerequisite order rows exist for the trade FK.
	var orderID1, orderID2 string
	err := pool.QueryRow(ctx,
		`INSERT INTO orders (
			maker, token_id, maker_amount, taker_amount, salt, side,
			signature, status, order_type, market_id, signature_hash
		) VALUES ('0xmaker1', 'tok', 100, 100, 'salt1-'||$1, 0, 'sig1-'||$1, 0, 0,
			'00000000-0000-0000-0000-000000000000', 'hash1-'||$1)
		RETURNING id`, matchID,
	).Scan(&orderID1)
	if err != nil {
		t.Fatalf("inserting test order 1: %v", err)
	}
	err = pool.QueryRow(ctx,
		`INSERT INTO orders (
			maker, token_id, maker_amount, taker_amount, salt, side,
			signature, status, order_type, market_id, signature_hash
		) VALUES ('0xmaker2', 'tok', 100, 100, 'salt2-'||$1, 1, 'sig2-'||$1, 0, 0,
			'00000000-0000-0000-0000-000000000000', 'hash2-'||$1)
		RETURNING id`, matchID,
	).Scan(&orderID2)
	if err != nil {
		t.Fatalf("inserting test order 2: %v", err)
	}

	var tradeID string
	err = pool.QueryRow(ctx,
		`INSERT INTO trades (
			match_id, maker_order_id, taker_order_id, maker_address,
			taker_address, market_id, price, size, maker_fee, taker_fee
		) VALUES ($1, $2, $3, '0xmaker1', '0xmaker2',
			'00000000-0000-0000-0000-000000000000', 50, 100, 5, 5)
		RETURNING id`, matchID, orderID1, orderID2,
	).Scan(&tradeID)
	if err != nil {
		t.Fatalf("inserting test trade: %v", err)
	}
	return tradeID
}

func TestPGRepository_CreateReferral(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	ref := &Referral{
		ReferrerAddress: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ReferredAddress: "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	if err := repo.CreateReferral(ctx, ref); err != nil {
		t.Fatalf("creating referral: %v", err)
	}
}

func TestPGRepository_CreateReferral_Duplicate(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	ref := &Referral{
		ReferrerAddress: "0xcccccccccccccccccccccccccccccccccccccccc",
		ReferredAddress: "0xdddddddddddddddddddddddddddddddddddddd",
	}
	if err := repo.CreateReferral(ctx, ref); err != nil {
		t.Fatalf("creating referral: %v", err)
	}

	err := repo.CreateReferral(ctx, ref)
	if !errors.Is(err, ErrDuplicateReferral) {
		t.Errorf("expected ErrDuplicateReferral, got: %v", err)
	}
}

func TestPGRepository_CreateReferral_SelfReferral(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	ref := &Referral{
		ReferrerAddress: "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		ReferredAddress: "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
	}
	err := repo.CreateReferral(ctx, ref)
	if !errors.Is(err, ErrSelfReferral) {
		t.Errorf("expected ErrSelfReferral, got: %v", err)
	}
}

func TestPGRepository_CreateReferral_Circular(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	// A refers B.
	ref := &Referral{
		ReferrerAddress: "0x1111111111111111111111111111111111111111",
		ReferredAddress: "0x2222222222222222222222222222222222222222",
	}
	if err := repo.CreateReferral(ctx, ref); err != nil {
		t.Fatalf("creating referral A→B: %v", err)
	}

	// B tries to refer A — should fail.
	reverse := &Referral{
		ReferrerAddress: "0x2222222222222222222222222222222222222222",
		ReferredAddress: "0x1111111111111111111111111111111111111111",
	}
	err := repo.CreateReferral(ctx, reverse)
	if !errors.Is(err, ErrCircularReferral) {
		t.Errorf("expected ErrCircularReferral, got: %v", err)
	}
}

func TestPGRepository_RecordEarning(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	tradeID := insertTestTrade(t, pool, "match-earn-1")

	earning := &Earning{
		ReferrerAddress: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TradeID:         tradeID,
		FeeAmount:       100,
		ReferrerCut:     20,
	}
	if err := repo.RecordEarning(ctx, earning); err != nil {
		t.Fatalf("recording earning: %v", err)
	}
}

func TestPGRepository_RecordEarning_Duplicate(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	tradeID := insertTestTrade(t, pool, "match-earn-dup")

	earning := &Earning{
		ReferrerAddress: "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		TradeID:         tradeID,
		FeeAmount:       100,
		ReferrerCut:     20,
	}
	if err := repo.RecordEarning(ctx, earning); err != nil {
		t.Fatalf("recording earning: %v", err)
	}

	err := repo.RecordEarning(ctx, earning)
	if !errors.Is(err, ErrDuplicateEarning) {
		t.Errorf("expected ErrDuplicateEarning, got: %v", err)
	}
}

func TestPGRepository_GetEarningsByReferrer(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	referrer := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	for i, matchID := range []string{"match-list-1", "match-list-2"} {
		tradeID := insertTestTrade(t, pool, matchID)
		earning := &Earning{
			ReferrerAddress: referrer,
			TradeID:         tradeID,
			FeeAmount:       int64(100 + i*50),
			ReferrerCut:     int64(20 + i*10),
		}
		if err := repo.RecordEarning(ctx, earning); err != nil {
			t.Fatalf("recording earning %d: %v", i, err)
		}
	}

	earnings, err := repo.GetEarningsByReferrer(ctx, referrer)
	if err != nil {
		t.Fatalf("getting earnings: %v", err)
	}
	if len(earnings) != 2 {
		t.Errorf("expected 2 earnings, got %d", len(earnings))
	}
}

func TestPGRepository_GetClaimableBalance(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	referrer := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	for _, matchID := range []string{"match-bal-1", "match-bal-2"} {
		tradeID := insertTestTrade(t, pool, matchID)
		earning := &Earning{
			ReferrerAddress: referrer,
			TradeID:         tradeID,
			FeeAmount:       100,
			ReferrerCut:     25,
		}
		if err := repo.RecordEarning(ctx, earning); err != nil {
			t.Fatalf("recording earning: %v", err)
		}
	}

	bal, err := repo.GetClaimableBalance(ctx, referrer)
	if err != nil {
		t.Fatalf("getting balance: %v", err)
	}
	if bal.TotalEarned != 50 {
		t.Errorf("total earned = %d, want 50", bal.TotalEarned)
	}
	if bal.Claimable != 50 {
		t.Errorf("claimable = %d, want 50", bal.Claimable)
	}
}

func TestPGRepository_GetClaimableBalance_NoEarnings(t *testing.T) {
	t.Parallel()
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	bal, err := repo.GetClaimableBalance(ctx, "0x0000000000000000000000000000000000000000")
	if err != nil {
		t.Fatalf("getting balance: %v", err)
	}
	if bal.TotalEarned != 0 {
		t.Errorf("total earned = %d, want 0", bal.TotalEarned)
	}
	if bal.Claimable != 0 {
		t.Errorf("claimable = %d, want 0", bal.Claimable)
	}
}
