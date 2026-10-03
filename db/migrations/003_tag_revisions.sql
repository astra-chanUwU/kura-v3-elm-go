-- Optimistic tag edits and immutable Inspector history.
ALTER TABLE posts
    ADD COLUMN IF NOT EXISTS tag_version INTEGER NOT NULL DEFAULT 0
        CHECK (tag_version >= 0);

CREATE TABLE IF NOT EXISTS post_tag_revisions (
    post_id BIGINT NOT NULL REFERENCES posts(id),
    version INTEGER NOT NULL CHECK (version > 0),
    kind TEXT NOT NULL,
    added_tags TEXT[] NOT NULL DEFAULT '{}'::text[],
    removed_tags TEXT[] NOT NULL DEFAULT '{}'::text[],
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (post_id, version)
);

CREATE INDEX IF NOT EXISTS post_tag_revisions_post_created_idx
    ON post_tag_revisions (post_id, created_at DESC, version DESC);
