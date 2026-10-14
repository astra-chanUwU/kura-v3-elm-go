-- Versioned source/artist edits with immutable Inspector history.
ALTER TABLE posts
    ADD COLUMN IF NOT EXISTS metadata_version INTEGER NOT NULL DEFAULT 0
        CHECK (metadata_version >= 0);

CREATE TABLE IF NOT EXISTS post_metadata_revisions (
    post_id BIGINT NOT NULL REFERENCES posts(id),
    version INTEGER NOT NULL CHECK (version > 0),
    previous_source TEXT NOT NULL,
    previous_artist TEXT NOT NULL,
    source TEXT NOT NULL,
    artist TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (post_id, version)
);

CREATE INDEX IF NOT EXISTS post_metadata_revisions_post_created_idx
    ON post_metadata_revisions (post_id, created_at DESC, version DESC);
