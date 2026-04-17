package affiliate

import (
	"fmt"

	"github.com/Shisa-Fosho/services/internal/shared/eth"
)

// ValidateReferral checks that a referral meets all creation rules.
// Returns ErrInvalidReferral, ErrSelfReferral, or nil.
func ValidateReferral(ref *Referral) error {
	if !eth.IsValidAddress(ref.ReferrerAddress) {
		return fmt.Errorf("invalid referrer address %q: %w", ref.ReferrerAddress, ErrInvalidReferral)
	}

	if !eth.IsValidAddress(ref.ReferredAddress) {
		return fmt.Errorf("invalid referred address %q: %w", ref.ReferredAddress, ErrInvalidReferral)
	}

	if ref.ReferrerAddress == ref.ReferredAddress {
		return fmt.Errorf("referrer and referred cannot be the same address: %w", ErrSelfReferral)
	}

	return nil
}

// ValidateEarning checks that an earning meets all persistence rules.
// Callers must invoke ValidateEarning before RecordEarning.
// Returns ErrInvalidEarning wrapping a descriptive message on failure.
func ValidateEarning(earning *Earning) error {
	if !eth.IsValidAddress(earning.ReferrerAddress) {
		return fmt.Errorf("invalid referrer address %q: %w", earning.ReferrerAddress, ErrInvalidEarning)
	}

	if earning.TradeID == "" {
		return fmt.Errorf("trade id is required: %w", ErrInvalidEarning)
	}

	if earning.FeeAmount <= 0 {
		return fmt.Errorf("fee amount must be positive, got %d: %w", earning.FeeAmount, ErrInvalidEarning)
	}

	if earning.ReferrerCut <= 0 {
		return fmt.Errorf("referrer cut must be positive, got %d: %w", earning.ReferrerCut, ErrInvalidEarning)
	}

	return nil
}
