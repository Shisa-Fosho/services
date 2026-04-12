package engine

import (
	"testing"

	"github.com/Shisa-Fosho/services/internal/trading"
)

// mkResting builds a resting order for tests. The id, side, price, and size
// are the only things most tests care about; other fields are zero-valued.
func mkResting(id string, side trading.Side, price int64, size int64, seq uint64) *restingOrder {
	return &restingOrder{
		Order:         &trading.Order{ID: id},
		CanonicalSide: side,
		Price:         price,
		RemainingSize: size,
		SeqNum:        seq,
	}
}

func TestBook_EmptyBestBidAsk(t *testing.T) {
	t.Parallel()

	b := newBook("m-1", trading.TokenPair{YesTokenID: "y", NoTokenID: "n"})
	if b.bestBid() != nil {
		t.Error("expected nil bestBid on empty book")
	}
	if b.bestAsk() != nil {
		t.Error("expected nil bestAsk on empty book")
	}
}

func TestBook_AddResting_BidSortedDescending(t *testing.T) {
	t.Parallel()

	b := newBook("m-1", trading.TokenPair{})

	// Insert bids out of order; expect best (highest) at index 0.
	b.addResting(mkResting("o1", trading.SideBuy, 40, 10, 1))
	b.addResting(mkResting("o2", trading.SideBuy, 60, 10, 2))
	b.addResting(mkResting("o3", trading.SideBuy, 50, 10, 3))

	if len(b.bids) != 3 {
		t.Fatalf("expected 3 bid levels, got %d", len(b.bids))
	}
	if b.bids[0].Price != 60 {
		t.Errorf("best bid = %d, want 60", b.bids[0].Price)
	}
	if b.bids[1].Price != 50 {
		t.Errorf("bids[1] = %d, want 50", b.bids[1].Price)
	}
	if b.bids[2].Price != 40 {
		t.Errorf("bids[2] = %d, want 40", b.bids[2].Price)
	}
	if b.bestBid().Price != 60 {
		t.Errorf("bestBid().Price = %d, want 60", b.bestBid().Price)
	}
}

func TestBook_AddResting_AskSortedAscending(t *testing.T) {
	t.Parallel()

	b := newBook("m-1", trading.TokenPair{})

	b.addResting(mkResting("a1", trading.SideSell, 60, 10, 1))
	b.addResting(mkResting("a2", trading.SideSell, 50, 10, 2))
	b.addResting(mkResting("a3", trading.SideSell, 55, 10, 3))

	if len(b.asks) != 3 {
		t.Fatalf("expected 3 ask levels, got %d", len(b.asks))
	}
	if b.asks[0].Price != 50 {
		t.Errorf("best ask = %d, want 50", b.asks[0].Price)
	}
	if b.asks[1].Price != 55 {
		t.Errorf("asks[1] = %d, want 55", b.asks[1].Price)
	}
	if b.asks[2].Price != 60 {
		t.Errorf("asks[2] = %d, want 60", b.asks[2].Price)
	}
	if b.bestAsk().Price != 50 {
		t.Errorf("bestAsk().Price = %d, want 50", b.bestAsk().Price)
	}
}

func TestBook_AddResting_SamePriceFIFO(t *testing.T) {
	t.Parallel()

	b := newBook("m-1", trading.TokenPair{})

	// Three bids at the same price — expect a single level with FIFO order.
	b.addResting(mkResting("first", trading.SideBuy, 50, 10, 1))
	b.addResting(mkResting("second", trading.SideBuy, 50, 10, 2))
	b.addResting(mkResting("third", trading.SideBuy, 50, 10, 3))

	if len(b.bids) != 1 {
		t.Fatalf("expected 1 bid level (merged by price), got %d", len(b.bids))
	}
	level := b.bids[0]
	if len(level.Orders) != 3 {
		t.Fatalf("expected 3 orders at level, got %d", len(level.Orders))
	}
	if level.Orders[0].Order.ID != "first" {
		t.Errorf("FIFO head = %q, want first", level.Orders[0].Order.ID)
	}
	if level.Orders[2].Order.ID != "third" {
		t.Errorf("FIFO tail = %q, want third", level.Orders[2].Order.ID)
	}
}

func TestBook_AddResting_RegistersByID(t *testing.T) {
	t.Parallel()

	b := newBook("m-1", trading.TokenPair{})
	b.addResting(mkResting("byid-1", trading.SideBuy, 50, 10, 1))

	if _, ok := b.byID["byid-1"]; !ok {
		t.Error("addResting did not register order in byID map")
	}
}

func TestBook_RemoveOrder_FromMiddleOfLevel(t *testing.T) {
	t.Parallel()

	b := newBook("m-1", trading.TokenPair{})
	b.addResting(mkResting("r1", trading.SideBuy, 50, 10, 1))
	b.addResting(mkResting("r2", trading.SideBuy, 50, 10, 2))
	b.addResting(mkResting("r3", trading.SideBuy, 50, 10, 3))

	b.removeOrder("r2")

	if len(b.bids) != 1 {
		t.Fatalf("expected level to still exist, got %d levels", len(b.bids))
	}
	level := b.bids[0]
	if len(level.Orders) != 2 {
		t.Fatalf("expected 2 orders after remove, got %d", len(level.Orders))
	}
	if level.Orders[0].Order.ID != "r1" || level.Orders[1].Order.ID != "r3" {
		t.Errorf("after remove: %v, want [r1, r3]", []string{level.Orders[0].Order.ID, level.Orders[1].Order.ID})
	}
	if _, ok := b.byID["r2"]; ok {
		t.Error("byID still contains removed order")
	}
}

func TestBook_RemoveOrder_LastInLevelRemovesLevel(t *testing.T) {
	t.Parallel()

	b := newBook("m-1", trading.TokenPair{})
	b.addResting(mkResting("only-50", trading.SideSell, 50, 10, 1))
	b.addResting(mkResting("at-60", trading.SideSell, 60, 10, 2))

	b.removeOrder("only-50")

	if len(b.asks) != 1 {
		t.Fatalf("expected 1 level after removing only order at 50, got %d", len(b.asks))
	}
	if b.asks[0].Price != 60 {
		t.Errorf("remaining ask level = %d, want 60", b.asks[0].Price)
	}
}

func TestBook_RemoveOrder_Missing_NoOp(t *testing.T) {
	t.Parallel()

	b := newBook("m-1", trading.TokenPair{})
	b.addResting(mkResting("present", trading.SideBuy, 50, 10, 1))

	// Should be a no-op, not a panic.
	b.removeOrder("ghost")

	if len(b.bids) != 1 {
		t.Errorf("book mutated after no-op remove; bids = %d", len(b.bids))
	}
}

func TestBook_PopFilledHead_RemovesLevelWhenEmpty(t *testing.T) {
	t.Parallel()

	b := newBook("m-1", trading.TokenPair{})
	b.addResting(mkResting("lone", trading.SideSell, 50, 10, 1))
	b.addResting(mkResting("other", trading.SideSell, 60, 10, 2))

	b.popFilledHead(trading.SideSell)

	if len(b.asks) != 1 {
		t.Fatalf("expected 1 ask level after pop, got %d", len(b.asks))
	}
	if b.asks[0].Price != 60 {
		t.Errorf("remaining ask = %d, want 60", b.asks[0].Price)
	}
	if _, ok := b.byID["lone"]; ok {
		t.Error("byID still contains popped head order")
	}
}

func TestBook_PopFilledHead_KeepsLevelWhenMoreOrders(t *testing.T) {
	t.Parallel()

	b := newBook("m-1", trading.TokenPair{})
	b.addResting(mkResting("h1", trading.SideBuy, 50, 10, 1))
	b.addResting(mkResting("h2", trading.SideBuy, 50, 10, 2))

	b.popFilledHead(trading.SideBuy)

	if len(b.bids) != 1 {
		t.Fatalf("expected level to survive, got %d", len(b.bids))
	}
	if len(b.bids[0].Orders) != 1 {
		t.Fatalf("expected 1 order remaining, got %d", len(b.bids[0].Orders))
	}
	if b.bids[0].Orders[0].Order.ID != "h2" {
		t.Errorf("remaining head = %q, want h2", b.bids[0].Orders[0].Order.ID)
	}
}
