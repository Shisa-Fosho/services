package affiliate

import "context"

// Repository defines the persistence interface for the affiliate domain.
// Implementations must be safe for concurrent use.
type Repository interface {
	// CreateReferral persists a new referral relationship. Returns
	// ErrDuplicateReferral if the relationship already exists,
	// ErrCircularReferral if the reverse relationship exists.
	CreateReferral(ctx context.Context, ref *Referral) error

	// RecordEarning persists a new affiliate earning. Returns
	// ErrDuplicateEarning if the trade ID already exists (idempotency).
	RecordEarning(ctx context.Context, earning *Earning) error

	// GetEarningsByReferrer returns all earnings for a referrer,
	// ordered by creation time descending.
	GetEarningsByReferrer(ctx context.Context, referrerAddress string) ([]*Earning, error)

	// GetClaimableBalance returns the aggregate claimable balance for a
	// referrer. Returns a zero-value balance (not an error) if no earnings exist.
	GetClaimableBalance(ctx context.Context, referrerAddress string) (*ClaimableBalance, error)
}
