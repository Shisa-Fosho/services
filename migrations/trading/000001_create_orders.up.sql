CREATE TABLE IF NOT EXISTS orders (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    maker           TEXT        NOT NULL,
    token_id        TEXT        NOT NULL,
    maker_amount    BIGINT      NOT NULL,
    taker_amount    BIGINT      NOT NULL,
    salt            TEXT        NOT NULL,
    expiration      BIGINT      NOT NULL DEFAULT 0,
    nonce           BIGINT      NOT NULL DEFAULT 0,
    fee_rate_bps    BIGINT      NOT NULL DEFAULT 0,
    side            SMALLINT    NOT NULL,
    signature_type  SMALLINT    NOT NULL DEFAULT 0,
    signature       TEXT        NOT NULL,
    status          SMALLINT    NOT NULL DEFAULT 0,
    order_type      SMALLINT    NOT NULL DEFAULT 0,
    market_id       UUID        NOT NULL,
    signature_hash  TEXT        NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT orders_signature_hash_unique UNIQUE (signature_hash)
);

CREATE INDEX idx_orders_user_status ON orders (maker, status);
CREATE INDEX idx_orders_market_status ON orders (market_id, status);
