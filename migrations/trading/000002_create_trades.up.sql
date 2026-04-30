CREATE TABLE IF NOT EXISTS trades (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    match_id        TEXT        NOT NULL,
    maker_order_id  UUID        NOT NULL REFERENCES orders(id),
    taker_order_id  UUID        NOT NULL REFERENCES orders(id),
    maker_address   TEXT        NOT NULL,
    taker_address   TEXT        NOT NULL,
    market_id       UUID        NOT NULL,
    price           BIGINT      NOT NULL,
    size            BIGINT      NOT NULL,
    fee             BIGINT      NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT trades_match_id_unique UNIQUE (match_id)
);

CREATE INDEX idx_trades_maker_address ON trades (maker_address);
CREATE INDEX idx_trades_taker_address ON trades (taker_address);
CREATE INDEX idx_trades_market_id ON trades (market_id);
CREATE INDEX idx_trades_market_maker ON trades (market_id, maker_address);
CREATE INDEX idx_trades_market_taker ON trades (market_id, taker_address);
CREATE INDEX idx_trades_created_at ON trades (created_at);
