package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Shisa-Fosho/services/internal/platform/data"
)

// PGRepository implements APIKeyRepository using PostgreSQL via pgx.
// Queries operate on the api_keys table (whose schema lives in migrations/
// trading/ after the service split — see migration 000004).
type PGRepository struct {
	pool *pgxpool.Pool
}

// NewPGRepository creates a new PostgreSQL-backed API-key repository.
func NewPGRepository(pool *pgxpool.Pool) *PGRepository {
	return &PGRepository{pool: pool}
}

// UpsertAPIKey creates or updates an API key. On conflict (same key_hash),
// updates expires_at, hmac_secret_encrypted, and passphrase_hash —
// enabling idempotent re-derivation when the client presents the same
// EIP-712 signature inputs.
func (r *PGRepository) UpsertAPIKey(ctx context.Context, key *APIKey) error {
	if err := ValidateAPIKey(key); err != nil {
		return fmt.Errorf("upserting api key: %w", err)
	}
	_, err := r.pool.Exec(ctx,
		`INSERT INTO api_keys (key_hash, user_address, hmac_secret_encrypted, passphrase_hash, label, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (key_hash)
		 DO UPDATE SET expires_at = $6, hmac_secret_encrypted = $3, passphrase_hash = $4
		 WHERE api_keys.user_address = $2`,
		key.KeyHash, key.UserAddress, key.HMACSecretEncrypted, key.PassphraseHash, key.Label, key.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("upserting api key for %s: %w", key.UserAddress, err)
	}
	return nil
}

// GetAPIKeyByHash retrieves a single non-revoked, non-expired API key by its
// hash. Implements APIKeyReader so the HMAC middleware (same package) can use
// this repository directly for L2 verification.
func (r *PGRepository) GetAPIKeyByHash(ctx context.Context, keyHash string) (*APIKey, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM api_keys
		 WHERE key_hash = $1 AND revoked = false AND expires_at > now()`,
		keyHash,
	)
	if err != nil {
		return nil, fmt.Errorf("getting api key by hash: %w", err)
	}
	key, err := pgx.CollectExactlyOneRow(rows, pgx.RowToAddrOfStructByName[APIKey])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, data.ErrNotFound
		}
		return nil, fmt.Errorf("scanning api key by hash: %w", err)
	}
	return key, nil
}

// GetAPIKeysByUser returns all non-revoked, non-expired API keys for a user,
// ordered by created_at descending (newest first).
func (r *PGRepository) GetAPIKeysByUser(ctx context.Context, userAddress string) ([]*APIKey, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT * FROM api_keys
		 WHERE user_address = $1 AND revoked = false AND expires_at > now()
		 ORDER BY created_at DESC`,
		userAddress,
	)
	if err != nil {
		return nil, fmt.Errorf("listing api keys for %s: %w", userAddress, err)
	}
	keys, err := pgx.CollectRows(rows, pgx.RowToAddrOfStructByName[APIKey])
	if err != nil {
		return nil, fmt.Errorf("scanning api keys for %s: %w", userAddress, err)
	}
	return keys, nil
}

// RevokeAPIKey marks an API key as revoked by its key_hash, scoped by user.
// Returns data.ErrNotFound if the key does not exist or does not belong to
// the given user.
func (r *PGRepository) RevokeAPIKey(ctx context.Context, keyHash string, userAddress string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE api_keys SET revoked = true
		 WHERE key_hash = $1 AND user_address = $2 AND revoked = false`,
		keyHash, userAddress,
	)
	if err != nil {
		return fmt.Errorf("revoking api key: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("revoking api key %s: %w", keyHash, data.ErrNotFound)
	}
	return nil
}
