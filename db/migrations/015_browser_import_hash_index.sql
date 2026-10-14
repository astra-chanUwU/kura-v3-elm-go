-- Bound exact-duplicate lookups to visible posts, ordered by stable ID.
CREATE INDEX IF NOT EXISTS posts_visible_hash_id_idx
    ON posts (hash, id)
    WHERE deleted_at IS NULL AND moderation_state = 'published';
