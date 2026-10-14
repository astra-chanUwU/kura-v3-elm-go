-- Content ratings are explicit moderator/editor input. Existing and newly
-- uploaded posts stay unrated until a user assigns one of the other values.
ALTER TABLE posts
    ADD COLUMN IF NOT EXISTS rating TEXT NOT NULL DEFAULT 'unrated'
        CHECK (rating IN ('unrated', 'safe', 'questionable', 'explicit')),
    ADD COLUMN IF NOT EXISTS rating_version INTEGER NOT NULL DEFAULT 0
        CHECK (rating_version >= 0);

CREATE TABLE IF NOT EXISTS post_rating_revisions (
    post_id BIGINT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
    version INTEGER NOT NULL CHECK (version >= 0),
    previous_rating TEXT NOT NULL CHECK (previous_rating IN ('unrated', 'safe', 'questionable', 'explicit')),
    rating TEXT NOT NULL CHECK (rating IN ('unrated', 'safe', 'questionable', 'explicit')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (post_id, version)
);

CREATE INDEX IF NOT EXISTS post_rating_revisions_post_created_idx
    ON post_rating_revisions (post_id, created_at DESC, version DESC);

-- Match the published-post browser query's rating predicate and newest order.
CREATE INDEX IF NOT EXISTS posts_rating_id_idx
    ON posts (rating, id DESC)
    WHERE deleted_at IS NULL AND moderation_state = 'published';
