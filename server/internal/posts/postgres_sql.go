package posts

// SearchPostsSQL is the handwritten query contract for the PostgreSQL
// adapter. $1 is websearch text; $2 is the optional favorite value; each
// operator/value pair is optional for score, width, and height; $9 is the
// optional keyset id; and $10 is limit+1 so the adapter can detect another
// page. Operators are validated by ParseQuery before they reach this query.
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
  AND ($2::boolean IS NULL OR favorite = $2::boolean)
  AND ($3::text IS NULL OR
       ($3::text = '>' AND score > $4::integer) OR
       ($3::text = '>=' AND score >= $4::integer) OR
       ($3::text = '<' AND score < $4::integer) OR
       ($3::text = '<=' AND score <= $4::integer) OR
       ($3::text = '=' AND score = $4::integer))
  AND ($5::text IS NULL OR
       ($5::text = '>' AND width > $6::integer) OR
       ($5::text = '>=' AND width >= $6::integer) OR
       ($5::text = '<' AND width < $6::integer) OR
       ($5::text = '<=' AND width <= $6::integer) OR
       ($5::text = '=' AND width = $6::integer))
  AND ($7::text IS NULL OR
       ($7::text = '>' AND height > $8::integer) OR
       ($7::text = '>=' AND height >= $8::integer) OR
       ($7::text = '<' AND height < $8::integer) OR
       ($7::text = '<=' AND height <= $8::integer) OR
       ($7::text = '=' AND height = $8::integer))
  AND ($9::bigint IS NULL OR id < $9::bigint)
ORDER BY id DESC
LIMIT $10
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
// target_tags IS NOT NULL preserves NULL versus valid empty tags distinction.
const GetPostRevisionsSQL = `
SELECT version, kind, added_tags, removed_tags, target_tags IS NOT NULL, target_tags, created_at::text
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
INSERT INTO post_tag_revisions (post_id, version, kind, added_tags, removed_tags, target_tags)
VALUES ($1::bigint, $2, 'tag_edit', $3, $4, $5)
`

const lockPostForTagRevertSQL = `
SELECT tags, search_text, tag_version
FROM posts
WHERE id = $1::bigint AND deleted_at IS NULL
FOR UPDATE
`

const targetTagsForRevisionSQL = `
SELECT target_tags IS NOT NULL, target_tags
FROM post_tag_revisions
WHERE post_id = $1::bigint AND version = $2
`

const insertPostRevertRevisionSQL = `
INSERT INTO post_tag_revisions (post_id, version, kind, added_tags, removed_tags, target_tags)
VALUES ($1::bigint, $2, 'tag_revert', $3, $4, $5)
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

const listCollectionsSQL = `
SELECT c.id::text, c.name, COALESCE(array_agg(cp.post_id::text ORDER BY cp.position, cp.post_id) FILTER (WHERE cp.post_id IS NOT NULL), ARRAY[]::text[])
FROM collections c
LEFT JOIN collection_posts cp ON cp.collection_id = c.id
LEFT JOIN posts p ON p.id = cp.post_id AND p.deleted_at IS NULL
WHERE cp.post_id IS NULL OR p.id IS NOT NULL
GROUP BY c.id, c.name
ORDER BY c.created_at, c.id
`

const insertCollectionSQL = `
INSERT INTO collections (name) VALUES ($1)
RETURNING id::text, name
`

const collectionExistsSQL = `SELECT 1 FROM collections WHERE id = $1::bigint`

const addCollectionPostSQL = `
INSERT INTO collection_posts (collection_id, post_id, position)
SELECT $1::bigint, p.id, COALESCE((SELECT MAX(position) + 1 FROM collection_posts WHERE collection_id = $1::bigint), 0) + row_number() OVER ()
FROM posts p
WHERE p.id = ANY($2::bigint[]) AND p.deleted_at IS NULL
ON CONFLICT (collection_id, post_id) DO NOTHING
`

const collectionPostIDsSQL = `
SELECT post_id::text FROM collection_posts cp
JOIN posts p ON p.id = cp.post_id AND p.deleted_at IS NULL
WHERE cp.collection_id = $1::bigint
ORDER BY cp.position, cp.post_id
`

const searchCollectionPostsSQL = `
SELECT
    p.id::text,
    p.preview_url,
    p.original_url,
    p.media_type,
    p.width,
    p.height,
    p.tags
FROM collection_posts cp
JOIN posts p ON p.id = cp.post_id AND p.deleted_at IS NULL
WHERE cp.collection_id = $1::bigint
ORDER BY cp.position, cp.post_id
LIMIT $2
`

const deleteCollectionPostSQL = `
DELETE FROM collection_posts cp USING posts p
WHERE cp.collection_id = $1::bigint AND cp.post_id = $2::bigint AND p.id = cp.post_id AND p.deleted_at IS NULL
`
