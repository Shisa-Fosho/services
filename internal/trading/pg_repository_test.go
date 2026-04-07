//go:build integration

package trading

import (
	"context"
	"errors"
	"fmt"
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

// cleanTables truncates trading tables between tests.
func cleanTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	// Order matters due to FK constraints: trades references orders.
	_, err := pool.Exec(ctx, "TRUNCATE trades, orders, balances")
	if err != nil {
		t.Fatalf("truncating tables: %v", err)
	}
}

func TestPGRepository_SaveAndGetOrder(t *testing.T) {
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	order := &Order{
		Maker:         "0xabc123",
		TokenID:       "token-yes-1",
		MakerAmount:   40,
		TakerAmount:   60,
		Salt:          "salt-1",
		Expiration:    time.Now().Add(time.Hour).Unix(),
		Nonce:         1,
		FeeRateBps:    100,
		Side:          SideBuy,
		SignatureType: 0,
		Signature:     "0xsig1",
		Status:        OrderStatusOpen,
		OrderType:     OrderTypeGTC,
		MarketID:      "a0000000-0000-0000-0000-000000000001",
		SignatureHash: "hash-1",
	}

	err := repo.SaveOrder(ctx, order)
	if err != nil {
		t.Fatalf("SaveOrder: %v", err)
	}

	// List to find the generated ID.
	orders, err := repo.ListOrdersByUser(ctx, "0xabc123", nil)
	if err != nil {
		t.Fatalf("ListOrdersByUser: %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("expected 1 order, got %d", len(orders))
	}

	got, err := repo.GetOrder(ctx, orders[0].ID)
	if err != nil {
		t.Fatalf("GetOrder: %v", err)
	}
	if got.Maker != "0xabc123" {
		t.Errorf("Maker = %q, want %q", got.Maker, "0xabc123")
	}
	if got.MakerAmount != 40 {
		t.Errorf("MakerAmount = %d, want 40", got.MakerAmount)
	}
	if got.Status != OrderStatusOpen {
		t.Errorf("Status = %v, want OPEN", got.Status)
	}
}

func TestPGRepository_SaveOrder_Duplicate(t *testing.T) {
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	order := &Order{
		Maker:         "0xabc123",
		TokenID:       "token-yes-1",
		MakerAmount:   40,
		TakerAmount:   60,
		Salt:          "salt-dup",
		Signature:     "0xsig-dup",
		Status:        OrderStatusOpen,
		OrderType:     OrderTypeGTC,
		MarketID:      "a0000000-0000-0000-0000-000000000001",
		SignatureHash: "hash-dup",
	}

	if err := repo.SaveOrder(ctx, order); err != nil {
		t.Fatalf("first SaveOrder: %v", err)
	}

	err := repo.SaveOrder(ctx, order)
	if !errors.Is(err, ErrDuplicateOrder) {
		t.Errorf("expected ErrDuplicateOrder, got: %v", err)
	}
}

func TestPGRepository_GetOrder_NotFound(t *testing.T) {
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, err := repo.GetOrder(ctx, "a0000000-0000-0000-0000-000000000099")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestPGRepository_ListOrdersByUser_StatusFilter(t *testing.T) {
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	for i, status := range []OrderStatus{OrderStatusOpen, OrderStatusFilled, OrderStatusOpen} {
		o := &Order{
			Maker:         "0xfilteruser",
			TokenID:       "token-1",
			MakerAmount:   40,
			TakerAmount:   60,
			Salt:          fmt.Sprintf("salt-filter-%d", i),
			Signature:     fmt.Sprintf("sig-filter-%d", i),
			Status:        status,
			OrderType:     OrderTypeGTC,
			MarketID:      "a0000000-0000-0000-0000-000000000001",
			SignatureHash: fmt.Sprintf("hash-filter-%d", i),
		}
		if err := repo.SaveOrder(ctx, o); err != nil {
			t.Fatalf("SaveOrder %d: %v", i, err)
		}
	}

	open, err := repo.ListOrdersByUser(ctx, "0xfilteruser", []OrderStatus{OrderStatusOpen})
	if err != nil {
		t.Fatalf("ListOrdersByUser: %v", err)
	}
	if len(open) != 2 {
		t.Errorf("expected 2 open orders, got %d", len(open))
	}

	all, err := repo.ListOrdersByUser(ctx, "0xfilteruser", nil)
	if err != nil {
		t.Fatalf("ListOrdersByUser (all): %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 total orders, got %d", len(all))
	}
}

func TestPGRepository_ListOrdersByMarket(t *testing.T) {
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	marketA := "a0000000-0000-0000-0000-000000000001"
	marketB := "b0000000-0000-0000-0000-000000000002"

	for i, mkt := range []string{marketA, marketA, marketB} {
		o := &Order{
			Maker:         "0xmaker",
			TokenID:       "token-1",
			MakerAmount:   50,
			TakerAmount:   50,
			Salt:          fmt.Sprintf("salt-mkt-%d", i),
			Signature:     fmt.Sprintf("sig-mkt-%d", i),
			Status:        OrderStatusOpen,
			OrderType:     OrderTypeGTC,
			MarketID:      mkt,
			SignatureHash: fmt.Sprintf("hash-mkt-%d", i),
		}
		if err := repo.SaveOrder(ctx, o); err != nil {
			t.Fatalf("SaveOrder %d: %v", i, err)
		}
	}

	got, err := repo.ListOrdersByMarket(ctx, marketA, nil)
	if err != nil {
		t.Fatalf("ListOrdersByMarket: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 orders for market A, got %d", len(got))
	}
}

func TestPGRepository_UpdateOrderStatus(t *testing.T) {
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	o := &Order{
		Maker:         "0xstatus",
		TokenID:       "token-1",
		MakerAmount:   40,
		TakerAmount:   60,
		Salt:          "salt-status",
		Signature:     "sig-status",
		Status:        OrderStatusOpen,
		OrderType:     OrderTypeGTC,
		MarketID:      "a0000000-0000-0000-0000-000000000001",
		SignatureHash: "hash-status",
	}
	if err := repo.SaveOrder(ctx, o); err != nil {
		t.Fatalf("SaveOrder: %v", err)
	}

	orders, _ := repo.ListOrdersByUser(ctx, "0xstatus", nil)
	id := orders[0].ID

	if err := repo.UpdateOrderStatus(ctx, id, OrderStatusCancelled); err != nil {
		t.Fatalf("UpdateOrderStatus: %v", err)
	}

	got, _ := repo.GetOrder(ctx, id)
	if got.Status != OrderStatusCancelled {
		t.Errorf("status = %v, want CANCELLED", got.Status)
	}
}

func TestPGRepository_UpdateOrderStatus_NotFound(t *testing.T) {
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	err := repo.UpdateOrderStatus(ctx, "a0000000-0000-0000-0000-000000000099", OrderStatusCancelled)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestPGRepository_SaveTrade(t *testing.T) {
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	// Create two orders first (FK constraint).
	marketID := "a0000000-0000-0000-0000-000000000001"
	for i, sig := range []string{"sig-trade-maker", "sig-trade-taker"} {
		o := &Order{
			Maker:         fmt.Sprintf("0xtrader%d", i),
			TokenID:       "token-1",
			MakerAmount:   50,
			TakerAmount:   50,
			Salt:          fmt.Sprintf("salt-trade-%d", i),
			Signature:     sig,
			Status:        OrderStatusOpen,
			OrderType:     OrderTypeGTC,
			MarketID:      marketID,
			SignatureHash: fmt.Sprintf("hash-trade-%d", i),
		}
		if err := repo.SaveOrder(ctx, o); err != nil {
			t.Fatalf("SaveOrder %d: %v", i, err)
		}
	}

	orders, _ := repo.ListOrdersByMarket(ctx, marketID, nil)
	if len(orders) < 2 {
		t.Fatal("expected at least 2 orders")
	}

	trade := &Trade{
		MatchID:      "match-1",
		MakerOrderID: orders[0].ID,
		TakerOrderID: orders[1].ID,
		MakerAddress: orders[0].Maker,
		TakerAddress: orders[1].Maker,
		MarketID:     marketID,
		Price:        50,
		Size:         10,
		MakerFee:     1,
		TakerFee:     2,
	}

	if err := repo.SaveTrade(ctx, trade); err != nil {
		t.Fatalf("SaveTrade: %v", err)
	}

	// Duplicate should fail.
	err := repo.SaveTrade(ctx, trade)
	if !errors.Is(err, ErrDuplicateTrade) {
		t.Errorf("expected ErrDuplicateTrade, got: %v", err)
	}
}

func TestPGRepository_Balance_CreditAndReserve(t *testing.T) {
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()
	addr := "0xbalance1"

	// GetBalance for non-existent user returns zero.
	b, err := repo.GetBalance(ctx, addr)
	if err != nil {
		t.Fatalf("GetBalance: %v", err)
	}
	if b.Available != 0 || b.Reserved != 0 {
		t.Errorf("expected zero balance, got available=%d reserved=%d", b.Available, b.Reserved)
	}

	// Credit creates the row.
	if err := repo.CreditAvailable(ctx, addr, 1000); err != nil {
		t.Fatalf("CreditAvailable: %v", err)
	}

	b, _ = repo.GetBalance(ctx, addr)
	if b.Available != 1000 {
		t.Errorf("available = %d, want 1000", b.Available)
	}

	// Credit again adds to existing.
	if err := repo.CreditAvailable(ctx, addr, 500); err != nil {
		t.Fatalf("CreditAvailable (2nd): %v", err)
	}

	b, _ = repo.GetBalance(ctx, addr)
	if b.Available != 1500 {
		t.Errorf("available = %d, want 1500", b.Available)
	}

	// Reserve moves from available to reserved.
	if err := repo.ReserveBalance(ctx, addr, 400); err != nil {
		t.Fatalf("ReserveBalance: %v", err)
	}

	b, _ = repo.GetBalance(ctx, addr)
	if b.Available != 1100 || b.Reserved != 400 {
		t.Errorf("after reserve: available=%d reserved=%d, want 1100/400", b.Available, b.Reserved)
	}

	// Release moves from reserved back to available.
	if err := repo.ReleaseBalance(ctx, addr, 100); err != nil {
		t.Fatalf("ReleaseBalance: %v", err)
	}

	b, _ = repo.GetBalance(ctx, addr)
	if b.Available != 1200 || b.Reserved != 300 {
		t.Errorf("after release: available=%d reserved=%d, want 1200/300", b.Available, b.Reserved)
	}

	// DeductReserved removes from reserved (trade settled).
	if err := repo.DeductReserved(ctx, addr, 200); err != nil {
		t.Fatalf("DeductReserved: %v", err)
	}

	b, _ = repo.GetBalance(ctx, addr)
	if b.Available != 1200 || b.Reserved != 100 {
		t.Errorf("after deduct: available=%d reserved=%d, want 1200/100", b.Available, b.Reserved)
	}
	if b.Total() != 1300 {
		t.Errorf("total = %d, want 1300", b.Total())
	}
}

func TestPGRepository_ReserveBalance_InsufficientFunds(t *testing.T) {
	pool := testPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()
	addr := "0xpoor"

	// No balance row at all.
	err := repo.ReserveBalance(ctx, addr, 100)
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Errorf("expected ErrInsufficientFunds (no row), got: %v", err)
	}

	// Insufficient available.
	repo.CreditAvailable(ctx, addr, 50)
	err = repo.ReserveBalance(ctx, addr, 100)
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Errorf("expected ErrInsufficientFunds (insufficient), got: %v", err)
	}
}
