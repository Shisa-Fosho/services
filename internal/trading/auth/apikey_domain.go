package auth

import (
	"context"
	"errors"
	"time"
)

// ErrInvalidAPIKey is returned when API key validation fails (e.g. malformed
// key_hash, invalid user address, missing hmac secret, etc.).
var ErrInvalidAPIKey = errors.New("invalid api key")

// APIKey is the auth-layer view of a stored API credential. The HMAC secret
// is persisted in encrypted form; the passphrase is stored as a SHA-256 hash.
// The raw secret and raw passphrase are never persisted.
type APIKey struct {
	KeyHash             string    `db:"key_hash"`              // SHA-256 of the raw API key, hex-encoded. PK.
	UserAddress         string    `db:"user_address"`          // FK to users.address.
	HMACSecretEncrypted string    `db:"hmac_secret_encrypted"` // AES-256-GCM ciphertext.
	PassphraseHash      string    `db:"passphrase_hash"`       // SHA-256 of the passphrase, hex-encoded.
	Label               string    `db:"label"`
	ExpiresAt           time.Time `db:"expires_at"`
	Revoked             bool      `db:"revoked"`
	CreatedAt           time.Time `db:"created_at"`
}

// APIKeyReader is the narrow interface the AuthenticateAPIKey middleware needs:
// a way to look up a stored API key by its SHA-256 hash. Any storage backend
// (postgres, in-memory fake for tests) can satisfy this.
type APIKeyReader interface {
	GetAPIKeyByHash(ctx context.Context, keyHash string) (*APIKey, error)
}
