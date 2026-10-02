-- Read-only post detail metadata for the Inspector.
-- Existing rows receive deterministic-safe defaults; seed data may replace them.
ALTER TABLE posts
    ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS artist TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS hash TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS file_size BIGINT NOT NULL DEFAULT 0 CHECK (file_size >= 0),
    ADD COLUMN IF NOT EXISTS tags TEXT[] NOT NULL DEFAULT '{}'::text[];
