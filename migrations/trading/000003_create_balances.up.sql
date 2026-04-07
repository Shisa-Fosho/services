CREATE TABLE IF NOT EXISTS balances (
    user_address TEXT        PRIMARY KEY,
    available    BIGINT      NOT NULL DEFAULT 0,
    reserved     BIGINT      NOT NULL DEFAULT 0,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT balances_available_non_negative CHECK (available >= 0),
    CONSTRAINT balances_reserved_non_negative CHECK (reserved >= 0)
);
