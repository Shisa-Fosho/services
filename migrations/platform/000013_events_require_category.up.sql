-- Category is now a required relationship on every event. Any pre-existing
-- row with a null category_id is invalid under the new model and must be
-- fixed or removed manually before this migration runs; we do not backfill
-- silently.
ALTER TABLE events ALTER COLUMN category_id SET NOT NULL;
