// Package data defines the core domain types for user data and positions
// in the prediction market platform service.
package data

import (
	"errors"
	"time"
)

// Sentinel errors for the data domain.
var (
	ErrNotFound      = errors.New("not found")
	ErrDuplicateUser = errors.New("duplicate user")
	ErrInvalidUser   = errors.New("invalid user")
)

// SignupMethod represents how a user registered.
type SignupMethod int8

// SignupMethod values.
const (
	SignupMethodWallet SignupMethod = 0
	SignupMethodEmail  SignupMethod = 1
)

func (m SignupMethod) String() string {
	switch m {
	case SignupMethodWallet:
		return "WALLET"
	case SignupMethodEmail:
		return "EMAIL"
	default:
		return "UNKNOWN"
	}
}

// IsValid returns true if the signup method is a known value.
func (m SignupMethod) IsValid() bool {
	return m == SignupMethodWallet || m == SignupMethodEmail
}

// Side represents the direction of a position (BUY or SELL).
// Defined locally to avoid cross-domain imports; values match trading.Side.
type Side int8

// Side values.
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

// User represents a registered user of the platform.
type User struct {
	Address              string  // Ethereum address (0x-prefixed), PK.
	Username             string  // Unique display name.
	Email                *string // Nullable; required for email signup.
	SignupMethod         SignupMethod
	SafeAddress          string // Gnosis Safe address for wallet users.
	ProxyAddress         string // Poly Proxy address for email users.
	TwoFASecretEncrypted string // Encrypted 2FA secret (never logged).
	TwoFAEnabled         bool
	CreatedAt            time.Time
}

// Position represents a user's holding in a specific market and side.
// One row per user per market per side. Updated on each fill.
// All monetary amounts are in integer cents (1 = $0.01).
type Position struct {
	UserAddress       string // FK to users.address.
	MarketID          string // FK to markets.id.
	Side              Side
	Size              int64 // Number of contracts held.
	AverageEntryPrice int64 // Average price paid per contract, in cents.
	RealisedPnL       int64 // Cumulative realised profit/loss, in cents.
}
