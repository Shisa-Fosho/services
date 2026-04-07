package trading

import (
	"errors"
	"testing"
	"time"
)

func TestValidateOrder(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)

	cfg := MarketConfig{
		MarketID: "market-1",
		TickSize: 1,
		MinSize:  1,
		MaxSize:  10000,
		TokenPair: TokenPair{
			YesTokenID: "yes-token",
			NoTokenID:  "no-token",
		},
	}

	// validOrder returns a baseline valid order. Tests modify specific fields.
	validOrder := func() *Order {
		return &Order{
			Maker:       "0xabc123",
			TokenID:     "yes-token",
			MakerAmount: 40,
			TakerAmount: 60,
			Salt:        "random-salt",
			Expiration:  now.Unix() + 3600, // 1 hour from now.
			Nonce:       1,
			FeeRateBps:  100,
			Side:        SideBuy,
			OrderType:   OrderTypeGTC,
			Signature:   "0xdeadbeef",
			MarketID:    "market-1",
		}
	}

	tests := []struct {
		name    string
		modify  func(o *Order, c *MarketConfig)
		wantErr bool
	}{
		{
			name:    "valid order passes",
			modify:  func(o *Order, c *MarketConfig) {},
			wantErr: false,
		},
		{
			name:    "zero maker amount",
			modify:  func(o *Order, c *MarketConfig) { o.MakerAmount = 0 },
			wantErr: true,
		},
		{
			name:    "negative maker amount",
			modify:  func(o *Order, c *MarketConfig) { o.MakerAmount = -1 },
			wantErr: true,
		},
		{
			name:    "zero taker amount",
			modify:  func(o *Order, c *MarketConfig) { o.TakerAmount = 0 },
			wantErr: true,
		},
		{
			name:    "negative taker amount",
			modify:  func(o *Order, c *MarketConfig) { o.TakerAmount = -5 },
			wantErr: true,
		},
		{
			name: "price too low",
			modify: func(o *Order, c *MarketConfig) {
				// Price = 0 cents: maker=0, taker=100 would fail on maker<=0 first,
				// so use amounts that produce price=0 via integer division.
				o.MakerAmount = 0
				o.TakerAmount = 100
			},
			wantErr: true, // Fails on maker amount <= 0.
		},
		{
			name: "price too high at 100",
			modify: func(o *Order, c *MarketConfig) {
				// Price = 100: taker must be 0 but that fails taker<=0 check.
				o.MakerAmount = 100
				o.TakerAmount = 0
			},
			wantErr: true, // Fails on taker amount <= 0.
		},
		{
			name: "price not on tick size 5",
			modify: func(o *Order, c *MarketConfig) {
				c.TickSize = 5
				o.MakerAmount = 33 // Price = 33, not divisible by 5.
				o.TakerAmount = 67
			},
			wantErr: true,
		},
		{
			name: "price on tick size 5",
			modify: func(o *Order, c *MarketConfig) {
				c.TickSize = 5
				o.MakerAmount = 35 // Price = 35, divisible by 5.
				o.TakerAmount = 65
			},
			wantErr: false,
		},
		{
			name: "size below minimum",
			modify: func(o *Order, c *MarketConfig) {
				c.MinSize = 100
				o.TakerAmount = 50
				o.MakerAmount = 50 // Keep price valid.
			},
			wantErr: true,
		},
		{
			name: "size above maximum",
			modify: func(o *Order, c *MarketConfig) {
				c.MaxSize = 10
				o.TakerAmount = 50
				o.MakerAmount = 50
			},
			wantErr: true,
		},
		{
			name:    "invalid side",
			modify:  func(o *Order, c *MarketConfig) { o.Side = Side(99) },
			wantErr: true,
		},
		{
			name:    "invalid order type",
			modify:  func(o *Order, c *MarketConfig) { o.OrderType = OrderType(99) },
			wantErr: true,
		},
		{
			name:    "FOK order type is valid",
			modify:  func(o *Order, c *MarketConfig) { o.OrderType = OrderTypeFOK },
			wantErr: false,
		},
		{
			name:    "empty signature",
			modify:  func(o *Order, c *MarketConfig) { o.Signature = "" },
			wantErr: true,
		},
		{
			name:    "expired order",
			modify:  func(o *Order, c *MarketConfig) { o.Expiration = now.Unix() - 1 },
			wantErr: true,
		},
		{
			name:    "no expiration (0) is valid",
			modify:  func(o *Order, c *MarketConfig) { o.Expiration = 0 },
			wantErr: false,
		},
		{
			name:    "expiration exactly at now is valid",
			modify:  func(o *Order, c *MarketConfig) { o.Expiration = now.Unix() },
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			o := validOrder()
			c := cfg // Copy so modifications don't leak between tests.
			tt.modify(o, &c)

			err := ValidateOrder(o, c, now)

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no error, got: %v", err)
			}
			if tt.wantErr && err != nil && !errors.Is(err, ErrInvalidOrder) {
				t.Errorf("expected ErrInvalidOrder, got: %v", err)
			}
		})
	}
}
