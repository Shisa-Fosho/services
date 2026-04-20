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
		modify  func(order *Order, config *MarketConfig)
		wantErr bool
	}{
		{
			name:    "valid order passes",
			modify:  func(order *Order, config *MarketConfig) {},
			wantErr: false,
		},
		{
			name:    "zero maker amount",
			modify:  func(order *Order, config *MarketConfig) { order.MakerAmount = 0 },
			wantErr: true,
		},
		{
			name:    "negative maker amount",
			modify:  func(order *Order, config *MarketConfig) { order.MakerAmount = -1 },
			wantErr: true,
		},
		{
			name:    "zero taker amount",
			modify:  func(order *Order, config *MarketConfig) { order.TakerAmount = 0 },
			wantErr: true,
		},
		{
			name:    "negative taker amount",
			modify:  func(order *Order, config *MarketConfig) { order.TakerAmount = -5 },
			wantErr: true,
		},
		{
			name: "price too low",
			modify: func(order *Order, config *MarketConfig) {
				// Price = 0 cents: maker=0, taker=100 would fail on maker<=0 first,
				// so use amounts that produce price=0 via integer division.
				order.MakerAmount = 0
				order.TakerAmount = 100
			},
			wantErr: true, // Fails on maker amount <= 0.
		},
		{
			name: "price too high at 100",
			modify: func(order *Order, config *MarketConfig) {
				// Price = 100: taker must be 0 but that fails taker<=0 check.
				order.MakerAmount = 100
				order.TakerAmount = 0
			},
			wantErr: true, // Fails on taker amount <= 0.
		},
		{
			name: "price not on tick size 5",
			modify: func(order *Order, config *MarketConfig) {
				config.TickSize = 5
				order.MakerAmount = 33 // Price = 33, not divisible by 5.
				order.TakerAmount = 67
			},
			wantErr: true,
		},
		{
			name: "price on tick size 5",
			modify: func(order *Order, config *MarketConfig) {
				config.TickSize = 5
				order.MakerAmount = 35 // Price = 35, divisible by 5.
				order.TakerAmount = 65
			},
			wantErr: false,
		},
		{
			name: "size below minimum",
			modify: func(order *Order, config *MarketConfig) {
				config.MinSize = 100
				order.TakerAmount = 50
				order.MakerAmount = 50 // Keep price valid.
			},
			wantErr: true,
		},
		{
			name: "size above maximum",
			modify: func(order *Order, config *MarketConfig) {
				config.MaxSize = 10
				order.TakerAmount = 50
				order.MakerAmount = 50
			},
			wantErr: true,
		},
		{
			name:    "invalid side",
			modify:  func(order *Order, config *MarketConfig) { order.Side = Side(99) },
			wantErr: true,
		},
		{
			name:    "invalid order type",
			modify:  func(order *Order, config *MarketConfig) { order.OrderType = OrderType(99) },
			wantErr: true,
		},
		{
			name:    "FOK order type is valid",
			modify:  func(order *Order, config *MarketConfig) { order.OrderType = OrderTypeFOK },
			wantErr: false,
		},
		{
			name:    "empty signature",
			modify:  func(order *Order, config *MarketConfig) { order.Signature = "" },
			wantErr: true,
		},
		{
			name:    "expired order",
			modify:  func(order *Order, config *MarketConfig) { order.Expiration = now.Unix() - 1 },
			wantErr: true,
		},
		{
			name:    "no expiration (0) is valid",
			modify:  func(order *Order, config *MarketConfig) { order.Expiration = 0 },
			wantErr: false,
		},
		{
			name:    "expiration exactly at now is valid",
			modify:  func(order *Order, config *MarketConfig) { order.Expiration = now.Unix() },
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			order := validOrder()
			cfgCopy := cfg // Copy so modifications don't leak between tests.
			tt.modify(order, &cfgCopy)

			err := ValidateOrder(order, cfgCopy, now)

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
