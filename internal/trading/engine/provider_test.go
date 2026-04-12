package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/Shisa-Fosho/services/internal/trading"
)

func TestStaticProvider_GetMarketConfig(t *testing.T) {
	t.Parallel()

	cfg := trading.MarketConfig{
		MarketID: "m-1",
		TickSize: 1,
		MinSize:  1,
		MaxSize:  1000,
		TokenPair: trading.TokenPair{
			YesTokenID: "yes-1",
			NoTokenID:  "no-1",
		},
	}

	p := NewStaticProvider(map[string]trading.MarketConfig{"m-1": cfg})

	got, err := p.GetMarketConfig(context.Background(), "m-1")
	if err != nil {
		t.Fatalf("GetMarketConfig: %v", err)
	}
	if got != cfg {
		t.Errorf("got %+v, want %+v", got, cfg)
	}
}

func TestStaticProvider_NotFound(t *testing.T) {
	t.Parallel()

	p := NewStaticProvider(nil)
	_, err := p.GetMarketConfig(context.Background(), "missing")
	if !errors.Is(err, ErrMarketNotFound) {
		t.Errorf("expected ErrMarketNotFound, got %v", err)
	}
}

func TestStaticProvider_Set(t *testing.T) {
	t.Parallel()

	p := NewStaticProvider(nil)
	cfg := trading.MarketConfig{MarketID: "m-set", TickSize: 2}
	p.Set(cfg)

	got, err := p.GetMarketConfig(context.Background(), "m-set")
	if err != nil {
		t.Fatalf("GetMarketConfig after Set: %v", err)
	}
	if got.TickSize != 2 {
		t.Errorf("TickSize = %d, want 2", got.TickSize)
	}
}

func TestStaticProvider_InputCopied(t *testing.T) {
	t.Parallel()

	input := map[string]trading.MarketConfig{
		"m-copy": {MarketID: "m-copy", TickSize: 1},
	}
	p := NewStaticProvider(input)

	// Mutating the caller's map must not affect the provider.
	delete(input, "m-copy")

	if _, err := p.GetMarketConfig(context.Background(), "m-copy"); err != nil {
		t.Errorf("provider lost config after caller mutated input map: %v", err)
	}
}
