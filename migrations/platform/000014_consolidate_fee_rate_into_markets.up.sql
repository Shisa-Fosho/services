-- Move fee_rate_bps from market_fee_rates onto markets directly.
-- One value per market, no join needed when the publisher builds the
-- NATS KV payload. NULL means "use platform default" (currently 0 bps).
--
-- Bounds mirror the on-chain cap (MAX_FEE_RATE_BIPS = 1000 in
-- Polymarket/ctf-exchange::Fees.sol).

ALTER TABLE markets
    ADD COLUMN fee_rate_bps BIGINT
        CHECK (fee_rate_bps IS NULL OR fee_rate_bps BETWEEN 0 AND 1000);

UPDATE markets m
SET    fee_rate_bps = mfr.fee_rate_bps
FROM   market_fee_rates mfr
WHERE  m.id = mfr.market_id;

DROP TABLE market_fee_rates;
