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

const (
	SideBuy  Side = 0
	SideSell Side = 1
)

func (s Side) String() string {
	switch s {
	case SideBuy:
		return "BUY"
	case SideSell:
		return "SELL"
	default:
		return "UNKNOWN"
	}
}

// IsValid returns true if the side is BUY or SELL.
func (s Side) IsValid() bool {
	return s == SideBuy || s == SideSell
}

// OrderStatus represents the lifecycle state of an order.
type OrderStatus int8

const (
	OrderStatusOpen            OrderStatus = 0
	OrderStatusFilled          OrderStatus = 1
	OrderStatusPartiallyFilled OrderStatus = 2
	OrderStatusCancelled       OrderStatus = 3
)

func (s OrderStatus) String() string {
	switch s {
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
func (s OrderStatus) IsValid() bool {
	return s >= OrderStatusOpen && s <= OrderStatusCancelled
}

// OrderType represents the execution strategy of an order.
type OrderType int8

const (
	OrderTypeGTC OrderType = 0 // Good-Til-Cancelled: stays on book until filled or cancelled.
	OrderTypeFOK OrderType = 1 // Fill-Or-Kill: must fill entirely on arrival or be rejected.
)

func (t OrderType) String() string {
	switch t {
	case OrderTypeGTC:
		return "GTC"
	case OrderTypeFOK:
		return "FOK"
	default:
		return "UNKNOWN"
	}
}

// IsValid returns true if the order type is GTC or FOK.
func (t OrderType) IsValid() bool {
	return t == OrderTypeGTC || t == OrderTypeFOK
}

// Order represents a signed order submitted to the CLOB.
// Fields mirror Polymarket's EIP-712 signed order struct for SDK compatibility.
// All monetary amounts are in integer cents (1 = $0.01).
type Order struct {
	ID            string      // UUID, generated server-side.
	Maker         string      // Ethereum address (0x-prefixed, checksummed).
	TokenID       string      // Conditional token ID from CTFExchange.
	MakerAmount   int64       // Amount maker is offering, in integer cents.
	TakerAmount   int64       // Amount maker wants in return, in integer cents.
	Salt          string      // Random value for EIP-712 signature uniqueness.
	Expiration    int64       // Unix timestamp (seconds). 0 = no expiration.
	Nonce         int64       // User nonce for order invalidation.
	FeeRateBps    int64       // Fee in basis points (e.g., 100 = 1%).
	Side          Side        // BUY or SELL.
	SignatureType int8        // 0 = EOA, 1 = POLY_PROXY, 2 = POLY_GNOSIS_SAFE.
	Signature     string      // Hex-encoded EIP-712 signature.
	Status        OrderStatus // Current lifecycle status.
	OrderType     OrderType   // GTC or FOK.
	MarketID      string      // UUID of the market this order belongs to.
	SignatureHash string      // SHA-256 of Signature; idempotency key.
	CreatedAt     time.Time   // Set by database.
	UpdatedAt     time.Time   // Set by database.
}

// OrderPrice computes the price in integer cents from maker/taker amounts.
// Price = MakerAmount / (MakerAmount + TakerAmount) * 100.
// Returns 0 if total is zero (invalid order).
func OrderPrice(o *Order) int64 {
	total := o.MakerAmount + o.TakerAmount
	if total == 0 {
		return 0
	}
	return (o.MakerAmount * 100) / total
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
	MakerFee     int64     // Fee charged to maker, in cents.
	TakerFee     int64     // Fee charged to taker, in cents.
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
func (b Balance) Total() int64 {
	return b.Available + b.Reserved
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
