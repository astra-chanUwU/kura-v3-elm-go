-- Optimistic favorite and score reactions with immutable Inspector history.
ALTER TABLE posts
    ADD COLUMN IF NOT EXISTS favorite BOOLEAN NOT NULL DEFAULT false,
    ADD COLUMN IF NOT EXISTS score INTEGER NOT NULL DEFAULT 0
        CHECK (score >= 0),
    ADD COLUMN IF NOT EXISTS reaction_version INTEGER NOT NULL DEFAULT 0
        CHECK (reaction_version >= 0);

CREATE TABLE IF NOT EXISTS post_reaction_revisions (
    post_id BIGINT NOT NULL REFERENCES posts(id),
    version INTEGER NOT NULL CHECK (version > 0),
    favorite BOOLEAN NOT NULL,
    score INTEGER NOT NULL CHECK (score >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (post_id, version)
);

CREATE INDEX IF NOT EXISTS post_reaction_revisions_post_created_idx
    ON post_reaction_revisions (post_id, created_at DESC, version DESC);
