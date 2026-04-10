CREATE TABLE IF NOT EXISTS affiliate_earnings (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    referrer_address TEXT        NOT NULL,
    trade_id         UUID        NOT NULL,
    fee_amount       BIGINT      NOT NULL,
    referrer_cut     BIGINT      NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT affiliate_earnings_trade_unique UNIQUE (trade_id),
    CONSTRAINT affiliate_earnings_fee_positive CHECK (fee_amount > 0),
    CONSTRAINT affiliate_earnings_cut_positive CHECK (referrer_cut > 0)
);

CREATE INDEX idx_affiliate_earnings_referrer ON affiliate_earnings (referrer_address);
