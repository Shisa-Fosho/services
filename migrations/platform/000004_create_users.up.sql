CREATE TABLE IF NOT EXISTS users (
    address                TEXT        PRIMARY KEY,
    username               TEXT        NOT NULL,
    email                  TEXT,
    signup_method          SMALLINT    NOT NULL,
    safe_address           TEXT        NOT NULL DEFAULT '',
    proxy_address          TEXT        NOT NULL DEFAULT '',
    twofa_secret_encrypted TEXT        NOT NULL DEFAULT '',
    twofa_enabled          BOOLEAN     NOT NULL DEFAULT false,
    created_at             TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT users_username_unique UNIQUE (username),
    CONSTRAINT users_email_unique UNIQUE (email)
);

CREATE INDEX idx_users_email ON users (email) WHERE email IS NOT NULL;
