package posts

// SearchPostsSQL is the handwritten query contract for the PostgreSQL
// adapter. $1 is the normalized query (empty means newest browse), $2 is the
// optional keyset id, and $3 is limit+1 so the adapter can determine whether
// another page exists.
const SearchPostsSQL = `
SELECT
    id::text,
    preview_url,
    original_url,
    media_type,
    width,
    height,
    tags
FROM posts
WHERE deleted_at IS NULL
  AND ($1 = '' OR search_document @@ websearch_to_tsquery('simple', $1))
  AND ($2::bigint IS NULL OR id < $2::bigint)
ORDER BY id DESC
LIMIT $3
`

// GetPostDetailSQL fetches the complete visible Inspector shape. The ID is
// supplied as text so the HTTP/domain contract remains stable.
const GetPostDetailSQL = `
SELECT
    id::text,
    preview_url,
    original_url,
    media_type,
    width,
    height,
    source,
    artist,
    hash,
    file_size,
    created_at::text,
    tags,
    tag_version,
    favorite,
    score,
    reaction_version
FROM posts
WHERE deleted_at IS NULL
  AND id = $1::bigint
`

// GetPostRevisionsSQL returns the newest Inspector history entries.
const GetPostRevisionsSQL = `
SELECT version, kind, added_tags, removed_tags, created_at::text
FROM post_tag_revisions
WHERE post_id = $1::bigint
ORDER BY version DESC
LIMIT 50
`

// GetPostReactionRevisionsSQL returns the newest favorite/score history
// entries for the Inspector.
const GetPostReactionRevisionsSQL = `
SELECT version, favorite, score, created_at::text
FROM post_reaction_revisions
WHERE post_id = $1::bigint
ORDER BY version DESC
LIMIT 50
`

const lockPostForTagEditSQL = `
SELECT tags, search_text, tag_version
FROM posts
WHERE id = $1::bigint AND deleted_at IS NULL
FOR UPDATE
`

const updatePostTagsSQL = `
UPDATE posts
SET tags = $1, search_text = $2, tag_version = $3
WHERE id = $4::bigint AND deleted_at IS NULL
`

const insertPostRevisionSQL = `
INSERT INTO post_tag_revisions (post_id, version, kind, added_tags, removed_tags)
VALUES ($1::bigint, $2, 'tag_edit', $3, $4)
`

const lockPostForReactionSQL = `
SELECT favorite, score, reaction_version
FROM posts
WHERE id = $1::bigint AND deleted_at IS NULL
FOR UPDATE
`

const updatePostReactionSQL = `
UPDATE posts
SET favorite = $1, score = $2, reaction_version = $3
WHERE id = $4::bigint AND deleted_at IS NULL
`

const insertPostReactionRevisionSQL = `
INSERT INTO post_reaction_revisions (post_id, version, favorite, score)
VALUES ($1::bigint, $2, $3, $4)
`
