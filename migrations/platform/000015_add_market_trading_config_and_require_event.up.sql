-- 1. Trading-config columns on markets. DEFAULTs are only used to backfill
--    pre-existing rows; new inserts must supply the values explicitly
--    (handler validation enforces this). DEFAULTs are dropped after backfill
--    so silent fallbacks never happen at the DB layer.
--
--    tick_size is an enum index: 0 = "0.1", 1 = "0.01", 2 = "0.001",
--    3 = "0.0001". Stored as SMALLINT to match existing Status / EventType /
--    Side patterns; the string mapping lives in Go.
ALTER TABLE markets
    ADD COLUMN tick_size SMALLINT NOT NULL DEFAULT 1
        CHECK (tick_size BETWEEN 0 AND 3),
    ADD COLUMN min_size  BIGINT   NOT NULL DEFAULT 5
        CHECK (min_size > 0),
    ADD COLUMN max_size  BIGINT
        CHECK (max_size IS NULL OR max_size >= min_size);

ALTER TABLE markets
    ALTER COLUMN tick_size DROP DEFAULT,
    ALTER COLUMN min_size  DROP DEFAULT;

-- 2. Tighten event_id to NOT NULL — every market belongs to an event.
--    Any orphan markets in lower envs must be deleted manually before
--    this migration runs; the up.sql intentionally fails loudly if
--    NULLs remain (rather than silently inventing a parent event).
ALTER TABLE markets
    ALTER COLUMN event_id SET NOT NULL;

-- 3. Per-market on-chain identity is unique.
ALTER TABLE markets
    ADD CONSTRAINT markets_condition_id_unique UNIQUE (condition_id);

-- 4. On-chain question identifiers.
--      events.neg_risk_market_id — nullable; populated only when event_type
--          = NEG_RISK. Groups multiple questions under one adapter-level
--          marketId.
--      markets.question_id      — NOT NULL and globally unique. Every CT
--          condition derives from a questionId, and the frontend needs it
--          to construct reportPayouts at resolve-time. ADD COLUMN NOT NULL
--          without a DEFAULT fails on a non-empty table; pre-existing market
--          rows must be truncated before this migration runs (matching the
--          event_id NOT NULL precedent above — fail loudly, no silent
--          invention).
ALTER TABLE events
    ADD COLUMN neg_risk_market_id TEXT;

ALTER TABLE markets
    ADD COLUMN question_id TEXT NOT NULL,
    ADD CONSTRAINT markets_question_id_unique UNIQUE (question_id);
