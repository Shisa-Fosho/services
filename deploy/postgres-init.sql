-- Initialize Shisa database extensions.
-- The 'shisa' database and user are created by POSTGRES_DB / POSTGRES_USER env vars.

CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";
