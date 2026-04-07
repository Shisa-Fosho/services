package trading

import (
	"fmt"
	"time"
)

// MarketConfig holds market-specific parameters that govern order validation.
// These values come from the Platform Service — validation never fetches or
// hardcodes them. The caller is responsible for providing the correct config.
type MarketConfig struct {
	MarketID  string    // UUID of the market.
	TickSize  int64     // Minimum price increment in cents (e.g., 1 = $0.01).
	MinSize   int64     // Minimum order size in contracts.
	MaxSize   int64     // Maximum order size in contracts.
	TokenPair TokenPair // YES/NO token IDs for this market.
}

// ValidateOrder checks that an order meets all market rules.
// now is the current time, passed explicitly for testability.
// Returns ErrInvalidOrder wrapping a descriptive message on failure.
func ValidateOrder(order *Order, cfg MarketConfig, now time.Time) error {
	// Amounts must be positive.
	if order.MakerAmount <= 0 {
		return fmt.Errorf("maker amount must be positive, got %d: %w", order.MakerAmount, ErrInvalidOrder)
	}
	if order.TakerAmount <= 0 {
		return fmt.Errorf("taker amount must be positive, got %d: %w", order.TakerAmount, ErrInvalidOrder)
	}

	// Price must be 1..99 cents (exclusive of 0 and 100).
	price := OrderPrice(order)
	if price < 1 || price > 99 {
		return fmt.Errorf("price must be between 1 and 99 cents, got %d: %w", price, ErrInvalidOrder)
	}

	// Price must align to tick size.
	if cfg.TickSize > 0 && price%cfg.TickSize != 0 {
		return fmt.Errorf("price %d not on tick size %d: %w", price, cfg.TickSize, ErrInvalidOrder)
	}

	// Size (taker amount) must be within market bounds.
	if order.TakerAmount < cfg.MinSize {
		return fmt.Errorf("size %d below minimum %d: %w", order.TakerAmount, cfg.MinSize, ErrInvalidOrder)
	}
	if cfg.MaxSize > 0 && order.TakerAmount > cfg.MaxSize {
		return fmt.Errorf("size %d above maximum %d: %w", order.TakerAmount, cfg.MaxSize, ErrInvalidOrder)
	}

	// Side must be valid.
	if !order.Side.IsValid() {
		return fmt.Errorf("invalid side %d: %w", order.Side, ErrInvalidOrder)
	}

	// Order type must be valid.
	if !order.OrderType.IsValid() {
		return fmt.Errorf("invalid order type %d: %w", order.OrderType, ErrInvalidOrder)
	}

	// Signature must be present.
	if order.Signature == "" {
		return fmt.Errorf("signature is required: %w", ErrInvalidOrder)
	}

	// Expiration must not be in the past (0 means no expiration).
	if order.Expiration != 0 && order.Expiration < now.Unix() {
		return fmt.Errorf("order expired at %d, current time %d: %w", order.Expiration, now.Unix(), ErrInvalidOrder)
	}

	return nil
}
