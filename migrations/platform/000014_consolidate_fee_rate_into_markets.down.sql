CREATE TABLE IF NOT EXISTS market_fee_rates (
    market_id     UUID        PRIMARY KEY REFERENCES markets(id) ON DELETE CASCADE,
    fee_rate_bps  INTEGER     NOT NULL CHECK (fee_rate_bps BETWEEN 0 AND 1000),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO market_fee_rates (market_id, fee_rate_bps, updated_at)
SELECT id, fee_rate_bps, updated_at
FROM   markets
WHERE  fee_rate_bps IS NOT NULL;

ALTER TABLE markets DROP COLUMN fee_rate_bps;
