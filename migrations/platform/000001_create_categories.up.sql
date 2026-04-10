CREATE TABLE IF NOT EXISTS categories (
    id   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name TEXT NOT NULL,
    slug TEXT NOT NULL,

    CONSTRAINT categories_slug_unique UNIQUE (slug)
);
