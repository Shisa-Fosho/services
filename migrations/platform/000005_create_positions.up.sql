CREATE TABLE IF NOT EXISTS positions (
    user_address        TEXT     NOT NULL REFERENCES users(address),
    market_id           UUID     NOT NULL REFERENCES markets(id),
    side                SMALLINT NOT NULL,
    size                BIGINT   NOT NULL DEFAULT 0,
    average_entry_price BIGINT   NOT NULL DEFAULT 0,
    realised_pnl        BIGINT   NOT NULL DEFAULT 0,

    CONSTRAINT positions_pk PRIMARY KEY (user_address, market_id, side),
    CONSTRAINT positions_size_non_negative CHECK (size >= 0)
);

CREATE INDEX idx_positions_user ON positions (user_address);
CREATE INDEX idx_positions_market ON positions (market_id);
