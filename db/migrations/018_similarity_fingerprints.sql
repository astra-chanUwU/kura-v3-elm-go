-- Bounded visual-similarity fingerprints (dHash-v1).
--
-- Idempotent: every statement is safe to re-run, and the lexical runner
-- (scripts/db-migrate.sh applies db/migrations/*.sql in filename order)
-- picks this file up after 017_tag_taxonomy.sql with no script change.
--
-- Additive only: nothing here alters existing tables, and no statement
-- touches original media bytes. Fingerprints describe immutable originals;
-- the original stays under its content-addressed uploads/ key.
--
-- Binding: original_hash records the posts.hash the fingerprint was
-- computed from. Reads only match rows where the fingerprint's
-- original_hash still equals the post's current hash, so stale
-- fingerprints never match wrong bytes. Retry safety comes from the
-- post_id primary key: reprocessing one post upserts one row.
--
-- Hash encoding: fingerprint is BIT(64) holding the 64 difference-hash
-- bits in row-major order (rows top to bottom, within a row left to
-- right). Distance is bit_count(candidate # anchor) in SQL.

CREATE TABLE IF NOT EXISTS similarity_fingerprints (
    post_id BIGINT NOT NULL PRIMARY KEY REFERENCES posts (id) ON DELETE CASCADE,
    algorithm TEXT NOT NULL DEFAULT 'dhash-v1'
        CHECK (algorithm = 'dhash-v1'),
    fingerprint BIT(64) NOT NULL,
    original_hash TEXT NOT NULL
        CHECK (original_hash ~ '^sha256:[0-9a-f]{64}$'),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Candidate scans: all similarity reads filter on algorithm and join to
-- visible posts, so index the algorithm facet.
CREATE INDEX IF NOT EXISTS similarity_fingerprints_algorithm_idx
    ON similarity_fingerprints (algorithm, post_id);
