package engine

import (
	"testing"

	"github.com/Shisa-Fosho/services/internal/trading"
)

// These tests exercise the pure match loop directly against a Book — no
// engine, no repo, no NATS. Table-driven end-to-end matching through the
// engine lives in engine_test.go.

func TestBook_Match_SimpleFullFill(t *testing.T) {
	t.Parallel()

	b := newBook("m-1", trading.TokenPair{})
	b.addResting(mkResting("maker1", trading.SideSell, 60, 10, 1))

	incoming := mkResting("taker1", trading.SideBuy, 60, 10, 2)
	fills := b.match(incoming)

	if len(fills) != 1 {
		t.Fatalf("expected 1 fill, got %d", len(fills))
	}
	if fills[0].FillSize != 10 {
		t.Errorf("fill size = %d, want 10", fills[0].FillSize)
	}
	if fills[0].Price != 60 {
		t.Errorf("fill price = %d, want 60 (maker price)", fills[0].Price)
	}
	if incoming.RemainingSize != 0 {
		t.Errorf("incoming RemainingSize = %d, want 0", incoming.RemainingSize)
	}
	if len(b.asks) != 0 {
		t.Errorf("expected asks to be empty after full fill, got %d levels", len(b.asks))
	}
}

func TestBook_Match_PriceImprovement(t *testing.T) {
	t.Parallel()

	// Resting SELL @ 60 cents. Incoming BUY @ 65 willing to pay more.
	// Execution must be at the maker price (60), not the taker price (65).
	b := newBook("m-1", trading.TokenPair{})
	b.addResting(mkResting("maker", trading.SideSell, 60, 10, 1))

	fills := b.match(mkResting("taker", trading.SideBuy, 65, 10, 2))

	if len(fills) != 1 || fills[0].Price != 60 {
		t.Errorf("expected price improvement to maker (60), got %+v", fills)
	}
}

func TestBook_Match_NoCross(t *testing.T) {
	t.Parallel()

	// Resting SELL @ 60. Incoming BUY @ 40 — does not cross.
	b := newBook("m-1", trading.TokenPair{})
	b.addResting(mkResting("maker", trading.SideSell, 60, 10, 1))

	incoming := mkResting("taker", trading.SideBuy, 40, 10, 2)
	fills := b.match(incoming)

	if len(fills) != 0 {
		t.Errorf("expected no fills on non-crossing order, got %d", len(fills))
	}
	if incoming.RemainingSize != 10 {
		t.Errorf("incoming should be untouched, RemainingSize = %d, want 10", incoming.RemainingSize)
	}
	if len(b.asks) != 1 {
		t.Errorf("asks should be unchanged, got %d levels", len(b.asks))
	}
}

func TestBook_Match_PartialFill(t *testing.T) {
	t.Parallel()

	// Resting SELL @ 60 size 30. Incoming BUY @ 60 size 100.
	// Expect one fill of size 30, incoming has 70 remaining, asks empty.
	b := newBook("m-1", trading.TokenPair{})
	b.addResting(mkResting("maker", trading.SideSell, 60, 30, 1))

	incoming := mkResting("taker", trading.SideBuy, 60, 100, 2)
	fills := b.match(incoming)

	if len(fills) != 1 || fills[0].FillSize != 30 {
		t.Fatalf("expected 1 fill of size 30, got %+v", fills)
	}
	if incoming.RemainingSize != 70 {
		t.Errorf("incoming RemainingSize = %d, want 70", incoming.RemainingSize)
	}
	if len(b.asks) != 0 {
		t.Errorf("maker should be fully consumed, asks = %d levels", len(b.asks))
	}
}

func TestBook_Match_SweepsMultipleLevels(t *testing.T) {
	t.Parallel()

	// Two resting sells at 60 (size 50) and 62 (size 100).
	// Incoming BUY @ 65 size 200 sweeps both levels, 50 remains.
	b := newBook("m-1", trading.TokenPair{})
	b.addResting(mkResting("m1", trading.SideSell, 60, 50, 1))
	b.addResting(mkResting("m2", trading.SideSell, 62, 100, 2))

	incoming := mkResting("taker", trading.SideBuy, 65, 200, 3)
	fills := b.match(incoming)

	if len(fills) != 2 {
		t.Fatalf("expected 2 fills, got %d", len(fills))
	}
	if fills[0].Price != 60 || fills[0].FillSize != 50 {
		t.Errorf("fill[0] = %+v, want price=60 size=50", fills[0])
	}
	if fills[1].Price != 62 || fills[1].FillSize != 100 {
		t.Errorf("fill[1] = %+v, want price=62 size=100", fills[1])
	}
	if incoming.RemainingSize != 50 {
		t.Errorf("incoming RemainingSize = %d, want 50", incoming.RemainingSize)
	}
	if len(b.asks) != 0 {
		t.Errorf("asks should be fully consumed, got %d levels", len(b.asks))
	}
}

func TestBook_Match_PriceTimePriority(t *testing.T) {
	t.Parallel()

	// Two sells at the same price (60), inserted in order m1 then m2.
	// Incoming BUY @ 60 size 10 should hit m1 first (older), not m2.
	b := newBook("m-1", trading.TokenPair{})
	b.addResting(mkResting("m1_older", trading.SideSell, 60, 10, 1))
	b.addResting(mkResting("m2_newer", trading.SideSell, 60, 10, 2))

	fills := b.match(mkResting("taker", trading.SideBuy, 60, 10, 3))

	if len(fills) != 1 {
		t.Fatalf("expected 1 fill, got %d", len(fills))
	}
	if fills[0].Maker.Order.ID != "m1_older" {
		t.Errorf("FIFO violated: matched %q, want m1_older", fills[0].Maker.Order.ID)
	}
}
