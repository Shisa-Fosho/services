// Package auth implements the trading service's Polymarket-compatible API-key
// lifecycle (derive, list, revoke) and the HMAC middleware that verifies every
// L2-authenticated trading request. The on-wire primitives (headers, typed
// data, EIP-712 verification, HMAC signing) all live in this package.
package auth

import "context"

// APIKeyRepository defines the trading service's persistence contract for
// API keys. Embedded APIKeyReader lets the HMAC middleware verify keys via
// the same backing store; the extra methods (upsert, list, revoke) are
// specific to this service's lifecycle endpoints.
type APIKeyRepository interface {
	APIKeyReader // GetAPIKeyByHash

	// UpsertAPIKey creates or updates an API key. On conflict (same key_hash),
	// updates expires_at, hmac_secret_encrypted, and passphrase_hash
	// (idempotent re-derivation). Validates input via ValidateAPIKey before
	// persisting. Returns ErrInvalidAPIKey for shape violations.
	UpsertAPIKey(ctx context.Context, key *APIKey) error

	// GetAPIKeysByUser returns all non-revoked, non-expired API keys for a user.
	GetAPIKeysByUser(ctx context.Context, userAddress string) ([]*APIKey, error)

	// RevokeAPIKey marks an API key as revoked by its key_hash, scoped to the
	// owning user. Returns ErrNotFound if the key does not exist or does not
	// belong to the given user.
	RevokeAPIKey(ctx context.Context, keyHash string, userAddress string) error
}
