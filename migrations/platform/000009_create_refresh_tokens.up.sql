CREATE TABLE IF NOT EXISTS refresh_tokens (
    id            TEXT        PRIMARY KEY,  -- JWT ID (jti claim).
    user_address  TEXT        NOT NULL REFERENCES users(address) ON DELETE CASCADE,
    expires_at    TIMESTAMPTZ NOT NULL,
    revoked       BOOLEAN     NOT NULL DEFAULT false,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_refresh_tokens_user_address ON refresh_tokens (user_address);
CREATE INDEX idx_refresh_tokens_active ON refresh_tokens (expires_at) WHERE revoked = false;
