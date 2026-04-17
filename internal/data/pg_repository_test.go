//go:build integration

package data

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Shisa-Fosho/services/internal/shared/eth"
	"github.com/Shisa-Fosho/services/internal/shared/postgres"
)

func cleanTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	// Truncate in reverse FK order.
	_, err := pool.Exec(ctx, `TRUNCATE positions, users CASCADE`)
	if err != nil {
		t.Fatalf("cleaning tables: %v", err)
	}
}

// insertTestMarket creates a market row needed for position FK constraints.
func insertTestMarket(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := pool.QueryRow(ctx,
		`INSERT INTO markets (
			slug, question, outcome_yes_label, outcome_no_label,
			token_id_yes, token_id_no, condition_id, status,
			price_yes, price_no, volume, open_interest
		) VALUES ('test-market-data', 'Test?', 'Yes', 'No', 'ty', 'tn', 'c1', 0, 50, 50, 0, 0)
		ON CONFLICT (slug) DO UPDATE SET slug = markets.slug
		RETURNING id`,
	).Scan(&id)
	if err != nil {
		t.Fatalf("inserting test market: %v", err)
	}
	return id
}

func TestPGRepository_CreateAndGetUser(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	user := &User{
		Address:      "0x1234567890abcdef1234567890abcdef12345678",
		Username:     "alice",
		SignupMethod: SignupMethodWallet,
	}
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("creating user: %v", err)
	}

	got, err := repo.GetUserByAddress(ctx, user.Address)
	if err != nil {
		t.Fatalf("getting user: %v", err)
	}
	if got.Address != user.Address {
		t.Errorf("address = %q, want %q", got.Address, user.Address)
	}
	if got.Username != "alice" {
		t.Errorf("username = %q, want %q", got.Username, "alice")
	}
	if got.SignupMethod != SignupMethodWallet {
		t.Errorf("signup method = %v, want %v", got.SignupMethod, SignupMethodWallet)
	}
}

func TestPGRepository_CreateUser_Duplicate(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	user := &User{
		Address:      "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Username:     "bob",
		SignupMethod: SignupMethodWallet,
	}
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("creating user: %v", err)
	}

	err := repo.CreateUser(ctx, user)
	if !errors.Is(err, ErrDuplicateUser) {
		t.Errorf("expected ErrDuplicateUser, got: %v", err)
	}
}

func TestPGRepository_GetUserByEmail(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	email := "charlie@example.com"
	user := &User{
		Address:      "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		Username:     "charlie",
		Email:        &email,
		SignupMethod: SignupMethodEmail,
	}
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("creating user: %v", err)
	}

	got, err := repo.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("getting user by email: %v", err)
	}
	if got.Address != user.Address {
		t.Errorf("address = %q, want %q", got.Address, user.Address)
	}
}

func TestPGRepository_GetUser_NotFound(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, err := repo.GetUserByAddress(ctx, "0x0000000000000000000000000000000000000000")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}

func TestPGRepository_UpsertPosition(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	marketID := insertTestMarket(t, pool)

	user := &User{
		Address:      "0xcccccccccccccccccccccccccccccccccccccccc",
		Username:     "posuser",
		SignupMethod: SignupMethodWallet,
	}
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("creating user: %v", err)
	}

	// Insert new position.
	pos := &Position{
		UserAddress:       user.Address,
		MarketID:          marketID,
		Side:              SideBuy,
		Size:              100,
		AverageEntryPrice: 45,
		RealisedPnL:       0,
	}
	if err := repo.UpsertPosition(ctx, pos); err != nil {
		t.Fatalf("upserting position: %v", err)
	}

	got, err := repo.GetPosition(ctx, user.Address, marketID, SideBuy)
	if err != nil {
		t.Fatalf("getting position: %v", err)
	}
	if got.Size != 100 {
		t.Errorf("size = %d, want 100", got.Size)
	}

	// Update existing position.
	pos.Size = 200
	pos.AverageEntryPrice = 50
	if err := repo.UpsertPosition(ctx, pos); err != nil {
		t.Fatalf("upserting position (update): %v", err)
	}

	got, err = repo.GetPosition(ctx, user.Address, marketID, SideBuy)
	if err != nil {
		t.Fatalf("getting updated position: %v", err)
	}
	if got.Size != 200 {
		t.Errorf("updated size = %d, want 200", got.Size)
	}
	if got.AverageEntryPrice != 50 {
		t.Errorf("updated avg price = %d, want 50", got.AverageEntryPrice)
	}
}

func TestPGRepository_GetPositionsByUser(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	marketID := insertTestMarket(t, pool)

	user := &User{
		Address:      eth.TestAddress(),
		Username:     "multipos",
		SignupMethod: SignupMethodWallet,
	}
	if err := repo.CreateUser(ctx, user); err != nil {
		t.Fatalf("creating user: %v", err)
	}

	for _, side := range []Side{SideBuy, SideSell} {
		pos := &Position{
			UserAddress:       user.Address,
			MarketID:          marketID,
			Side:              side,
			Size:              50,
			AverageEntryPrice: 50,
		}
		if err := repo.UpsertPosition(ctx, pos); err != nil {
			t.Fatalf("upserting position side %v: %v", side, err)
		}
	}

	positions, err := repo.GetPositionsByUser(ctx, user.Address)
	if err != nil {
		t.Fatalf("getting positions: %v", err)
	}
	if len(positions) != 2 {
		t.Errorf("expected 2 positions, got %d", len(positions))
	}
}

func TestPGRepository_GetPosition_NotFound(t *testing.T) {
	pool := postgres.TestPool(t)
	cleanTables(t, pool)
	repo := NewPGRepository(pool)
	ctx := context.Background()

	_, err := repo.GetPosition(ctx, "0x0000000000000000000000000000000000000000",
		"00000000-0000-0000-0000-000000000000", SideBuy)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got: %v", err)
	}
}
