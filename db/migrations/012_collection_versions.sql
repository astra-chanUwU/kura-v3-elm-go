-- Versioned collection pagination: membership/order version.
--
-- Idempotent: safe to re-run, and the lexical runner
-- (scripts/db-migrate.sh applies db/migrations/*.sql in filename order)
-- picks this file up after 011_asset_variants.sql with no script change.
--
-- Additive only: existing rows keep their bytes. The new version column
-- defaults to 0, so every pre-existing collection starts at a stable,
-- well-defined version. Add/remove/reorder mutations serialize on the
-- collection row and increment the version only for effective changes, so
-- readers can detect concurrent membership or order changes while paging.
ALTER TABLE collections
    ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 0
        CHECK (version >= 0);
