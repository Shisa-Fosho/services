CREATE TABLE IF NOT EXISTS events (
    id                  UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    slug                TEXT        NOT NULL,
    title               TEXT        NOT NULL,
    description         TEXT        NOT NULL DEFAULT '',
    category_id         UUID        REFERENCES categories(id),
    event_type          SMALLINT    NOT NULL,
    resolution_config   JSONB       NOT NULL DEFAULT '{}',
    status              SMALLINT    NOT NULL DEFAULT 0,
    end_date            TIMESTAMPTZ NOT NULL,
    featured            BOOLEAN     NOT NULL DEFAULT false,
    featured_sort_order SMALLINT    NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT events_slug_unique UNIQUE (slug)
);

CREATE INDEX idx_events_status ON events (status);
CREATE INDEX idx_events_category ON events (category_id);
CREATE INDEX idx_events_featured ON events (featured, featured_sort_order) WHERE featured = true;
