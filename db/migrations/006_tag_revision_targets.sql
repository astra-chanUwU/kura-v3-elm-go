-- Preserve the complete tag state needed to restore a historical revision.
ALTER TABLE post_tag_revisions
    ADD COLUMN IF NOT EXISTS target_tags TEXT[];

