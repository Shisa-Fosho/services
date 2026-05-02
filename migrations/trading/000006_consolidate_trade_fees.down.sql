-- No-op down: there is no faithful inverse — the original maker/taker
-- split was lost when #71 collapsed them into a single fee column. If
-- a rollback is ever needed, restore from a backup taken before #71.
SELECT 1;
