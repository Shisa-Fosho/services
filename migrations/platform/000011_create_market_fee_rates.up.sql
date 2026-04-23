-- Per-market fee rates, aligned with Polymarket's model
-- (Polymarket/ctf-exchange — Order.feeRateBps is signed into every EIP-712
-- order payload; rates are decided per-market by the operator).
--
-- Bounds mirror the on-chain cap (MAX_FEE_RATE_BIPS = 1000 in
-- src/exchange/mixins/Fees.sol). Above that the exchange reverts, so this
-- CHECK is a safety net for admins setting impossible rates.
CREATE TABLE IF NOT EXISTS market_fee_rates (
    market_id     UUID        PRIMARY KEY REFERENCES markets(id) ON DELETE CASCADE,
    fee_rate_bps  INTEGER     NOT NULL CHECK (fee_rate_bps BETWEEN 0 AND 1000),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
