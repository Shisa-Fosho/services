// Package trading defines the core domain types for the prediction market
// trading service: orders, trades, balances, and their associated enumerations.
package trading

import (
	"errors"
	"time"
)

// Sentinel errors for the trading domain.
var (
	ErrNotFound          = errors.New("not found")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrDuplicateOrder    = errors.New("duplicate order")
	ErrDuplicateTrade    = errors.New("duplicate trade")
	ErrOrderExpired      = errors.New("order expired")
	ErrInvalidOrder      = errors.New("invalid order")
)

// Side represents the direction of an order (BUY or SELL).
// Values match Polymarket's CTFExchange contract (0 = BUY, 1 = SELL).
type Side int8

// Side values.
const (
	SideBuy  Side = 0
	SideSell Side = 1
)

func (side Side) String() string {
	switch side {
	case SideBuy:
		return "BUY"
	case SideSell:
		return "SELL"
	default:
		return "UNKNOWN"
	}
}

// IsValid returns true if the side is BUY or SELL.
func (side Side) IsValid() bool {
	return side == SideBuy || side == SideSell
}

// OrderStatus represents the lifecycle state of an order.
type OrderStatus int8

// OrderStatus values.
const (
	OrderStatusOpen            OrderStatus = 0
	OrderStatusFilled          OrderStatus = 1
	OrderStatusPartiallyFilled OrderStatus = 2
	OrderStatusCancelled       OrderStatus = 3
)

func (status OrderStatus) String() string {
	switch status {
	case OrderStatusOpen:
		return "OPEN"
	case OrderStatusFilled:
		return "FILLED"
	case OrderStatusPartiallyFilled:
		return "PARTIALLY_FILLED"
	case OrderStatusCancelled:
		return "CANCELLED"
	default:
		return "UNKNOWN"
	}
}

// IsValid returns true if the status is a known value.
func (status OrderStatus) IsValid() bool {
	return status >= OrderStatusOpen && status <= OrderStatusCancelled
}

// OrderType represents the execution strategy of an order.
type OrderType int8

// OrderType values.
const (
	OrderTypeGTC OrderType = 0 // Good-Til-Cancelled: stays on book until filled or cancelled.
	OrderTypeFOK OrderType = 1 // Fill-Or-Kill: must fill entirely on arrival or be rejected.
)

func (orderType OrderType) String() string {
	switch orderType {
	case OrderTypeGTC:
		return "GTC"
	case OrderTypeFOK:
		return "FOK"
	default:
		return "UNKNOWN"
	}
}

// IsValid returns true if the order type is GTC or FOK.
func (orderType OrderType) IsValid() bool {
	return orderType == OrderTypeGTC || orderType == OrderTypeFOK
}

// Order represents a signed order submitted to the CLOB.
// Fields mirror Polymarket's EIP-712 signed order struct for SDK compatibility.
// All monetary amounts are in integer cents (1 = $0.01).
type Order struct {
	ID            string      `db:"id"`             // UUID, generated server-side.
	Maker         string      `db:"maker"`          // Ethereum address (0x-prefixed, checksummed).
	TokenID       string      `db:"token_id"`       // Conditional token ID from CTFExchange.
	MakerAmount   int64       `db:"maker_amount"`   // Amount maker is offering, in integer cents.
	TakerAmount   int64       `db:"taker_amount"`   // Amount maker wants in return, in integer cents.
	Salt          string      `db:"salt"`           // Random value for EIP-712 signature uniqueness.
	Expiration    int64       `db:"expiration"`     // Unix timestamp (seconds). 0 = no expiration.
	Nonce         int64       `db:"nonce"`          // User nonce for order invalidation.
	FeeRateBps    int64       `db:"fee_rate_bps"`   // Fee in basis points (e.g., 100 = 1%).
	Side          Side        `db:"side"`           // BUY or SELL.
	SignatureType int8        `db:"signature_type"` // 0 = EOA, 1 = POLY_PROXY, 2 = POLY_GNOSIS_SAFE.
	Signature     string      `db:"signature"`      // Hex-encoded EIP-712 signature.
	Status        OrderStatus `db:"status"`         // Current lifecycle status.
	OrderType     OrderType   `db:"order_type"`     // GTC or FOK.
	MarketID      string      `db:"market_id"`      // UUID of the market this order belongs to.
	SignatureHash string      `db:"signature_hash"` // SHA-256 of Signature; idempotency key.
	CreatedAt     time.Time   `db:"created_at"`     // Set by database.
	UpdatedAt     time.Time   `db:"updated_at"`     // Set by database.
}

// OrderPrice computes the price in integer cents from maker/taker amounts.
// Price = MakerAmount / (MakerAmount + TakerAmount) * 100.
// Returns 0 if total is zero (invalid order).
func OrderPrice(order *Order) int64 {
	total := order.MakerAmount + order.TakerAmount
	if total == 0 {
		return 0
	}
	return (order.MakerAmount * 100) / total
}

// Trade represents a matched trade between two orders.
// Addresses and market ID are denormalized to avoid JOINs on read-heavy queries.
// All monetary amounts are in integer cents (1 = $0.01).
type Trade struct {
	ID           string    // UUID, generated server-side.
	MatchID      string    // From CLOB engine; idempotency key.
	MakerOrderID string    // FK to orders.id.
	TakerOrderID string    // FK to orders.id.
	MakerAddress string    // Denormalized for query efficiency.
	TakerAddress string    // Denormalized for query efficiency.
	MarketID     string    // Denormalized for query efficiency.
	Price        int64     // Execution price in integer cents (1-99).
	Size         int64     // Number of contracts traded.
	Fee          int64     // Fee charged on the trade, from taker order's feeRateBps.
	CreatedAt    time.Time // Set by database.
}

// Balance represents a user's USDC balance for trading.
// All monetary amounts are in integer cents (1 = $0.01).
type Balance struct {
	UserAddress string    // PK, Ethereum address.
	Available   int64     // Funds free to place new orders, in cents.
	Reserved    int64     // Funds locked in open orders, in cents.
	UpdatedAt   time.Time // Set by database.
}

// Total returns the sum of available and reserved funds.
func (balance Balance) Total() int64 {
	return balance.Available + balance.Reserved
}

// TokenPair represents the YES and NO token IDs for a single market.
type TokenPair struct {
	YesTokenID string
	NoTokenID  string
}

// CanonicalOrder represents an order converted to canonical (YES-side) form.
// The unified order book stores all orders as YES-side so BUY and SELL can match.
type CanonicalOrder struct {
	Original       *Order // The original unconverted order.
	CanonicalSide  Side   // Side relative to YES token after conversion.
	CanonicalPrice int64  // Price in cents (1-99) in canonical form.
	WasConverted   bool   // True if the original was a NO-token order that was flipped.
}

// ToCanonical converts an order to canonical (YES-side) form.
//
// In a prediction market, BUY YES @ $0.40 is identical to SELL NO @ $0.60.
// The unified book converts all NO-token orders to their YES equivalents:
//
//	BUY  NO @ 60 cents → SELL YES @ 40 cents
//	SELL NO @ 60 cents → BUY  YES @ 40 cents
//
// YES-token orders pass through unchanged.
func ToCanonical(order *Order, pair TokenPair) CanonicalOrder {
	if order.TokenID == pair.YesTokenID {
		return CanonicalOrder{
			Original:       order,
			CanonicalSide:  order.Side,
			CanonicalPrice: OrderPrice(order),
			WasConverted:   false,
		}
	}

	var flippedSide Side

	if order.Side == SideBuy {
		flippedSide = SideSell
	} else {
		flippedSide = SideBuy
	}

	return CanonicalOrder{
		Original:       order,
		CanonicalSide:  flippedSide,
		CanonicalPrice: 100 - OrderPrice(order),
		WasConverted:   true,
	}
}
