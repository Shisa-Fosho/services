package affiliate

import (
	"fmt"

	"github.com/Shisa-Fosho/services/internal/platform/eth"
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
