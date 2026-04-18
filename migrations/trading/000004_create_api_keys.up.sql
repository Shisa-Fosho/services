CREATE TABLE IF NOT EXISTS api_keys (
    key_hash              TEXT        PRIMARY KEY,  -- SHA-256 of the raw API key, hex-encoded.
    user_address          TEXT        NOT NULL REFERENCES users(address) ON DELETE CASCADE,
    hmac_secret_encrypted TEXT        NOT NULL,     -- AES-256-GCM ciphertext of HMAC secret.
    label                 TEXT        NOT NULL DEFAULT '',
    expires_at            TIMESTAMPTZ NOT NULL,
    revoked               BOOLEAN     NOT NULL DEFAULT false,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_api_keys_user_address ON api_keys (user_address);
CREATE INDEX idx_api_keys_active ON api_keys (expires_at) WHERE revoked = false;
