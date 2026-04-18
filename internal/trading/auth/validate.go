package auth

import (
	"fmt"

	"github.com/Shisa-Fosho/services/internal/shared/eth"
)

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
