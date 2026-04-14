// Package data defines the core domain types for user data and positions
// in the prediction market platform service.
package data

import (
	"errors"
	"time"
)

// Sentinel errors for the data domain.
var (
	ErrNotFound        = errors.New("not found")
	ErrDuplicateUser   = errors.New("duplicate user")
	ErrInvalidUser     = errors.New("invalid user")
	ErrInvalidPosition = errors.New("invalid position")
	ErrTokenRevoked    = errors.New("token revoked")
	ErrTokenExpired    = errors.New("token expired")
	ErrInvalidAPIKey   = errors.New("invalid api key")
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
	Address              string       `db:"address"`  // Ethereum address (0x-prefixed), PK.
	Username             string       `db:"username"` // Unique display name.
	Email                *string      `db:"email"`    // Nullable; required for email signup.
	SignupMethod         SignupMethod `db:"signup_method"`
	SafeAddress          string       `db:"safe_address"`           // Gnosis Safe address for wallet users.
	ProxyAddress         string       `db:"proxy_address"`          // Poly Proxy address for email users.
	TwoFASecretEncrypted string       `db:"twofa_secret_encrypted"` // Encrypted 2FA secret (never logged).
	TwoFAEnabled         bool         `db:"twofa_enabled"`
	CreatedAt            time.Time    `db:"created_at"`
}

// RefreshToken represents a stored refresh token for session management.
type RefreshToken struct {
	ID          string    `db:"id"`           // JWT ID (jti).
	UserAddress string    `db:"user_address"` // FK to users.address.
	ExpiresAt   time.Time `db:"expires_at"`
	Revoked     bool      `db:"revoked"`
	CreatedAt   time.Time `db:"created_at"`
}

// APIKey represents a stored API key for programmatic access.
type APIKey struct {
	KeyHash             string    `db:"key_hash"`              // SHA-256 of the raw API key, hex-encoded. PK.
	UserAddress         string    `db:"user_address"`          // FK to users.address.
	HMACSecretEncrypted string    `db:"hmac_secret_encrypted"` // AES-256-GCM ciphertext.
	Label               string    `db:"label"`
	ExpiresAt           time.Time `db:"expires_at"`
	Revoked             bool      `db:"revoked"`
	CreatedAt           time.Time `db:"created_at"`
}

// Position represents a user's holding in a specific market and side.
// One row per user per market per side. Updated on each fill.
// All monetary amounts are in integer cents (1 = $0.01).
type Position struct {
	UserAddress       string `db:"user_address"` // FK to users.address.
	MarketID          string `db:"market_id"`    // FK to markets.id.
	Side              Side   `db:"side"`
	Size              int64  `db:"size"`                // Number of contracts held.
	AverageEntryPrice int64  `db:"average_entry_price"` // Average price paid per contract, in cents.
	RealisedPnL       int64  `db:"realised_pnl"`        // Cumulative realised profit/loss, in cents.
}
