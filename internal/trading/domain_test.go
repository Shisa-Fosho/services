package trading

import (
	"testing"
)

func TestSide_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		side Side
		want string
	}{
		{"buy", SideBuy, "BUY"},
		{"sell", SideSell, "SELL"},
		{"unknown", Side(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.side.String(); got != tt.want {
				t.Errorf("Side(%d).String() = %q, want %q", tt.side, got, tt.want)
			}
		})
	}
}

func TestSide_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		side Side
		want bool
	}{
		{"buy is valid", SideBuy, true},
		{"sell is valid", SideSell, true},
		{"negative is invalid", Side(-1), false},
		{"large is invalid", Side(99), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.side.IsValid(); got != tt.want {
				t.Errorf("Side(%d).IsValid() = %v, want %v", tt.side, got, tt.want)
			}
		})
	}
}

func TestOrderStatus_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status OrderStatus
		want   string
	}{
		{"open", OrderStatusOpen, "OPEN"},
		{"filled", OrderStatusFilled, "FILLED"},
		{"partially filled", OrderStatusPartiallyFilled, "PARTIALLY_FILLED"},
		{"cancelled", OrderStatusCancelled, "CANCELLED"},
		{"unknown", OrderStatus(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.status.String(); got != tt.want {
				t.Errorf("OrderStatus(%d).String() = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestOrderStatus_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status OrderStatus
		want   bool
	}{
		{"open", OrderStatusOpen, true},
		{"filled", OrderStatusFilled, true},
		{"partially filled", OrderStatusPartiallyFilled, true},
		{"cancelled", OrderStatusCancelled, true},
		{"negative", OrderStatus(-1), false},
		{"too large", OrderStatus(10), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.status.IsValid(); got != tt.want {
				t.Errorf("OrderStatus(%d).IsValid() = %v, want %v", tt.status, got, tt.want)
			}
		})
	}
}

func TestOrderType_String(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		orderType OrderType
		want      string
	}{
		{"gtc", OrderTypeGTC, "GTC"},
		{"fok", OrderTypeFOK, "FOK"},
		{"unknown", OrderType(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.orderType.String(); got != tt.want {
				t.Errorf("OrderType(%d).String() = %q, want %q", tt.orderType, got, tt.want)
			}
		})
	}
}

func TestOrderType_IsValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		orderType OrderType
		want      bool
	}{
		{"gtc", OrderTypeGTC, true},
		{"fok", OrderTypeFOK, true},
		{"negative", OrderType(-1), false},
		{"large", OrderType(5), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.orderType.IsValid(); got != tt.want {
				t.Errorf("OrderType(%d).IsValid() = %v, want %v", tt.orderType, got, tt.want)
			}
		})
	}
}

func TestBalance_Total(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		available int64
		reserved  int64
		want      int64
	}{
		{"both zero", 0, 0, 0},
		{"only available", 1000, 0, 1000},
		{"only reserved", 0, 500, 500},
		{"both non-zero", 1000, 500, 1500},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := Balance{Available: tt.available, Reserved: tt.reserved}
			if got := b.Total(); got != tt.want {
				t.Errorf("Balance{Available: %d, Reserved: %d}.Total() = %d, want %d",
					tt.available, tt.reserved, got, tt.want)
			}
		})
	}
}

func TestOrderPrice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		makerAmount int64
		takerAmount int64
		want        int64
	}{
		{"40 cents (40/100)", 40, 60, 40},
		{"60 cents (60/100)", 60, 40, 60},
		{"50 cents (50/100)", 50, 50, 50},
		{"1 cent (1/100)", 1, 99, 1},
		{"99 cents (99/100)", 99, 1, 99},
		{"zero total returns 0", 0, 0, 0},
		{"small amounts", 2, 3, 40},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			o := &Order{MakerAmount: tt.makerAmount, TakerAmount: tt.takerAmount}
			if got := OrderPrice(o); got != tt.want {
				t.Errorf("OrderPrice(maker=%d, taker=%d) = %d, want %d",
					tt.makerAmount, tt.takerAmount, got, tt.want)
			}
		})
	}
}

func TestToCanonical(t *testing.T) {
	t.Parallel()

	pair := TokenPair{
		YesTokenID: "yes-token-123",
		NoTokenID:  "no-token-456",
	}

	tests := []struct {
		name          string
		order         *Order
		wantSide      Side
		wantPrice     int64
		wantConverted bool
	}{
		{
			name: "BUY YES @ 40 unchanged",
			order: &Order{
				TokenID:     "yes-token-123",
				Side:        SideBuy,
				MakerAmount: 40,
				TakerAmount: 60,
			},
			wantSide:      SideBuy,
			wantPrice:     40,
			wantConverted: false,
		},
		{
			name: "SELL YES @ 40 unchanged",
			order: &Order{
				TokenID:     "yes-token-123",
				Side:        SideSell,
				MakerAmount: 40,
				TakerAmount: 60,
			},
			wantSide:      SideSell,
			wantPrice:     40,
			wantConverted: false,
		},
		{
			name: "BUY NO @ 60 becomes SELL YES @ 40",
			order: &Order{
				TokenID:     "no-token-456",
				Side:        SideBuy,
				MakerAmount: 60,
				TakerAmount: 40,
			},
			wantSide:      SideSell,
			wantPrice:     40,
			wantConverted: true,
		},
		{
			name: "SELL NO @ 60 becomes BUY YES @ 40",
			order: &Order{
				TokenID:     "no-token-456",
				Side:        SideSell,
				MakerAmount: 60,
				TakerAmount: 40,
			},
			wantSide:      SideBuy,
			wantPrice:     40,
			wantConverted: true,
		},
		{
			name: "edge case: NO @ 99 becomes YES @ 1",
			order: &Order{
				TokenID:     "no-token-456",
				Side:        SideBuy,
				MakerAmount: 99,
				TakerAmount: 1,
			},
			wantSide:      SideSell,
			wantPrice:     1,
			wantConverted: true,
		},
		{
			name: "edge case: NO @ 1 becomes YES @ 99",
			order: &Order{
				TokenID:     "no-token-456",
				Side:        SideSell,
				MakerAmount: 1,
				TakerAmount: 99,
			},
			wantSide:      SideBuy,
			wantPrice:     99,
			wantConverted: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ToCanonical(tt.order, pair)

			if got.CanonicalSide != tt.wantSide {
				t.Errorf("CanonicalSide = %v, want %v", got.CanonicalSide, tt.wantSide)
			}
			if got.CanonicalPrice != tt.wantPrice {
				t.Errorf("CanonicalPrice = %d, want %d", got.CanonicalPrice, tt.wantPrice)
			}
			if got.WasConverted != tt.wantConverted {
				t.Errorf("WasConverted = %v, want %v", got.WasConverted, tt.wantConverted)
			}
			if got.Original != tt.order {
				t.Error("Original pointer does not match input order")
			}
		})
	}
}
