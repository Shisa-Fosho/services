package data

import (
	"fmt"

	"github.com/Shisa-Fosho/services/internal/platform/eth"
)

// ValidateUser checks that a user meets all creation rules.
// Returns ErrInvalidUser wrapping a descriptive message on failure.
func ValidateUser(user *User) error {
	if !eth.IsValidAddress(user.Address) {
		return fmt.Errorf("invalid ethereum address %q: %w", user.Address, ErrInvalidUser)
	}

	if user.Username == "" {
		return fmt.Errorf("username is required: %w", ErrInvalidUser)
	}

	if !user.SignupMethod.IsValid() {
		return fmt.Errorf("invalid signup method %d: %w", user.SignupMethod, ErrInvalidUser)
	}

	if user.SignupMethod == SignupMethodEmail && (user.Email == nil || *user.Email == "") {
		return fmt.Errorf("email is required for email signup: %w", ErrInvalidUser)
	}

	return nil
}

// ValidateAPIKey checks that an API key meets all persistence rules.
// Returns ErrInvalidAPIKey wrapping a descriptive message on failure.
func ValidateAPIKey(key *APIKey) error {
	if key.KeyHash == "" {
		return fmt.Errorf("key hash is required: %w", ErrInvalidAPIKey)
	}
	if !eth.IsValidAddress(key.UserAddress) {
		return fmt.Errorf("invalid user address %q: %w", key.UserAddress, ErrInvalidAPIKey)
	}
	if key.HMACSecretEncrypted == "" {
		return fmt.Errorf("encrypted HMAC secret is required: %w", ErrInvalidAPIKey)
	}
	if key.PassphraseHash == "" {
		return fmt.Errorf("passphrase hash is required: %w", ErrInvalidAPIKey)
	}
	if key.ExpiresAt.IsZero() {
		return fmt.Errorf("expiry is required: %w", ErrInvalidAPIKey)
	}
	return nil
}

// ValidatePosition checks that a position meets all persistence rules.
// Callers must invoke ValidatePosition before UpsertPosition.
// Returns ErrInvalidPosition wrapping a descriptive message on failure.
func ValidatePosition(pos *Position) error {
	if !eth.IsValidAddress(pos.UserAddress) {
		return fmt.Errorf("invalid user address %q: %w", pos.UserAddress, ErrInvalidPosition)
	}

	if pos.MarketID == "" {
		return fmt.Errorf("market id is required: %w", ErrInvalidPosition)
	}

	if !pos.Side.IsValid() {
		return fmt.Errorf("invalid side %d: %w", pos.Side, ErrInvalidPosition)
	}

	if pos.Size < 0 {
		return fmt.Errorf("size must be non-negative, got %d: %w", pos.Size, ErrInvalidPosition)
	}

	return nil
}
