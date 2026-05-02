-- Cleanup migration for dev DBs that ran the pre-#71 version of
-- migrations/trading/000002_create_trades.up.sql, which created
-- maker_fee + taker_fee columns. PR #71 edited 000002 in place to use
-- a single `fee` column, so fresh DBs get the right shape — but dev
-- environments that had already passed migration 2 stayed on the old
-- shape with no migration to bring them forward.
--
-- On a fresh DB, both DROPs are no-ops via IF EXISTS. On a pre-#71 dev
-- DB, the legacy columns are dropped (fee already exists with default 0;
-- per-trade maker/taker breakdown is lost, which is acceptable since no
-- production environment ever ran this old shape).

ALTER TABLE trades DROP COLUMN IF EXISTS maker_fee;
ALTER TABLE trades DROP COLUMN IF EXISTS taker_fee;
