// Package auth provides the trading service's API-key auth handler and
// data layer. It owns the lifecycle of Polymarket-compatible API keys:
// derivation (from an EIP-712 wallet signature), listing, and revocation.
//
// API-key VERIFICATION (checking the HMAC signature on every trading request)
// lives in the shared package at internal/shared/auth — this package handles
// only the lifecycle operations that sit in front of the trading API.
package auth

import (
	"context"

	sharedauth "github.com/Shisa-Fosho/services/internal/shared/auth"
)

// APIKeyRepository defines the trading service's persistence contract for
// API keys. Embedded APIKeyReader lets shared/auth middleware verify keys
// via the same backing store; the extra methods (upsert, list, revoke)
// are specific to this service's lifecycle endpoints.
type APIKeyRepository interface {
	sharedauth.APIKeyReader // GetAPIKeyByHash

	// UpsertAPIKey creates or updates an API key. On conflict (same key_hash),
	// updates expires_at, hmac_secret_encrypted, and passphrase_hash
	// (idempotent re-derivation). Validates input via ValidateAPIKey before
	// persisting. Returns ErrInvalidAPIKey for shape violations.
	UpsertAPIKey(ctx context.Context, key *sharedauth.APIKey) error

	// GetAPIKeysByUser returns all non-revoked, non-expired API keys for a user.
	GetAPIKeysByUser(ctx context.Context, userAddress string) ([]*sharedauth.APIKey, error)

	// RevokeAPIKey marks an API key as revoked by its key_hash, scoped to the
	// owning user. Returns ErrNotFound if the key does not exist or does not
	// belong to the given user.
	RevokeAPIKey(ctx context.Context, keyHash string, userAddress string) error
}
