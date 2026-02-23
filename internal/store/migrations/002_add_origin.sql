ALTER TABLE events ADD COLUMN hostname TEXT;
ALTER TABLE events ADD COLUMN user_id TEXT;

UPDATE schema_version SET version = 2, applied_at = datetime('now') WHERE version = 1;
