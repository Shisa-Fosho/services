CREATE TABLE IF NOT EXISTS admin_wallets (
    address    TEXT        PRIMARY KEY,
    label      TEXT        NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
