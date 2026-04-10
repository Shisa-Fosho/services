package affiliate

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
)

// ValidateReferral checks that a referral meets all creation rules.
// Returns ErrInvalidReferral, ErrSelfReferral, or nil.
func ValidateReferral(ref *Referral) error {
	if len(ref.ReferrerAddress) != 42 || !common.IsHexAddress(ref.ReferrerAddress) {
		return fmt.Errorf("invalid referrer address %q: %w", ref.ReferrerAddress, ErrInvalidReferral)
	}

	if len(ref.ReferredAddress) != 42 || !common.IsHexAddress(ref.ReferredAddress) {
		return fmt.Errorf("invalid referred address %q: %w", ref.ReferredAddress, ErrInvalidReferral)
	}

	if ref.ReferrerAddress == ref.ReferredAddress {
		return fmt.Errorf("referrer and referred cannot be the same address: %w", ErrSelfReferral)
	}

	return nil
}
