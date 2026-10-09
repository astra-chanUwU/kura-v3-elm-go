-- Bounded thumbnail/derivative variants (unconnected slice).
--
-- Idempotent: every statement is safe to re-run, and the lexical runner
-- (scripts/db-migrate.sh applies db/migrations/*.sql in filename order)
-- picks this file up after 010_jobs.sql with no script change.
--
-- Additive only: nothing here alters existing tables, and no statement
-- touches original media bytes. Originals stay immutable under their
-- content-addressed uploads/ keys; derivatives live under derivatives/
-- keys recorded here. Retry safety comes from the (post_id, variant)
-- primary key: reprocessing the same variant upserts one row.
--
-- Status model: 'ready' rows carry usable variant metadata served to
-- readers. Terminal handler failures are reported through the durable
-- jobs row (last_error with retry/backoff), not by rewriting history.

CREATE TABLE IF NOT EXISTS asset_variants (
    post_id BIGINT NOT NULL REFERENCES posts (id) ON DELETE CASCADE,
    variant TEXT NOT NULL CHECK (length(btrim(variant)) BETWEEN 1 AND 64),
    object_key TEXT NOT NULL CHECK (length(btrim(object_key)) BETWEEN 1 AND 512),
    url TEXT NOT NULL CHECK (length(btrim(url)) BETWEEN 1 AND 1024),
    media_type TEXT NOT NULL CHECK (length(btrim(media_type)) BETWEEN 1 AND 128),
    width INTEGER NOT NULL CHECK (width > 0),
    height INTEGER NOT NULL CHECK (height > 0),
    file_size BIGINT NOT NULL CHECK (file_size >= 0),
    status TEXT NOT NULL DEFAULT 'ready'
        CHECK (status IN ('ready', 'failed')),
    last_error TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (post_id, variant)
);

-- Variant lookups per post (detail/Inspector reads).
CREATE INDEX IF NOT EXISTS asset_variants_post_idx
    ON asset_variants (post_id);

-- Status sweeps (failed-variant inspection without touching jobs).
CREATE INDEX IF NOT EXISTS asset_variants_status_updated_idx
    ON asset_variants (status, updated_at DESC, post_id ASC);
