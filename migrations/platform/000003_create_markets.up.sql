CREATE TABLE IF NOT EXISTS markets (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    slug              TEXT        NOT NULL,
    event_id          UUID        REFERENCES events(id),
    question          TEXT        NOT NULL,
    outcome_yes_label TEXT        NOT NULL DEFAULT 'Yes',
    outcome_no_label  TEXT        NOT NULL DEFAULT 'No',
    token_id_yes      TEXT        NOT NULL,
    token_id_no       TEXT        NOT NULL,
    condition_id      TEXT        NOT NULL,
    status            SMALLINT    NOT NULL DEFAULT 0,
    outcome           SMALLINT,
    price_yes         BIGINT      NOT NULL DEFAULT 50,
    price_no          BIGINT      NOT NULL DEFAULT 50,
    volume            BIGINT      NOT NULL DEFAULT 0,
    open_interest     BIGINT      NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT markets_slug_unique UNIQUE (slug),
    CONSTRAINT markets_volume_non_negative CHECK (volume >= 0),
    CONSTRAINT markets_open_interest_non_negative CHECK (open_interest >= 0)
);

CREATE INDEX idx_markets_event ON markets (event_id);
CREATE INDEX idx_markets_status ON markets (status);
