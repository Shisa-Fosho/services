-- Reverse 000015 in inverse order.

ALTER TABLE markets
    DROP CONSTRAINT IF EXISTS markets_question_id_unique;

ALTER TABLE markets
    DROP COLUMN IF EXISTS question_id;

ALTER TABLE events
    DROP COLUMN IF EXISTS neg_risk_market_id;

ALTER TABLE markets
    DROP CONSTRAINT IF EXISTS markets_condition_id_unique;

ALTER TABLE markets
    ALTER COLUMN event_id DROP NOT NULL;

ALTER TABLE markets
    DROP COLUMN IF EXISTS max_size,
    DROP COLUMN IF EXISTS min_size,
    DROP COLUMN IF EXISTS tick_size;
