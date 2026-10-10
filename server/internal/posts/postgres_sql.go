package posts

// Public visibility is centralized on one predicate: a post is publicly
// visible only when it is not soft-deleted AND its moderation_state is
// 'published'. Every query below that serves browser-facing reads or guards
// a mutation repeats "deleted_at IS NULL AND moderation_state = 'published'"
// inline so hidden, rejected, pending, or deleted rows are indistinguishable
// from missing rows. IsPubliclyVisibleModerationState in moderation.go is the
// Go side of the same rule. Moderation transitions lock rows with
// deleted_at IS NULL only, because they must reach non-published rows too.

// SearchPostsSQL is the newest-ordered query contract. $1 is websearch text;
// $2 is favorite; $3/$4 score; $5/$6 width; $7/$8 height; $9/$10 file_size;
// $11/$12 id; $13 media_type; $14 source; $15 artist; $16 included tags;
// $17 excluded tags; $18 keyset id; $19 limit+1. Operators are validated by
// ParseQuery before they reach SQL.
const SearchPostsSQL = `
SELECT
    id::text,
    preview_url,
    original_url,
    media_type,
    width,
    height,
    tags,
    score
FROM posts
WHERE deleted_at IS NULL
  AND moderation_state = 'published'
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
  AND ($9::text IS NULL OR
       ($9::text = '>' AND file_size > $10::bigint) OR
       ($9::text = '>=' AND file_size >= $10::bigint) OR
       ($9::text = '<' AND file_size < $10::bigint) OR
       ($9::text = '<=' AND file_size <= $10::bigint) OR
       ($9::text = '=' AND file_size = $10::bigint))
  AND ($11::text IS NULL OR
       ($11::text = '>' AND id > $12::bigint) OR
       ($11::text = '>=' AND id >= $12::bigint) OR
       ($11::text = '<' AND id < $12::bigint) OR
       ($11::text = '<=' AND id <= $12::bigint) OR
       ($11::text = '=' AND id = $12::bigint))
  AND ($13::text IS NULL OR media_type = $13::text)
  AND ($14::text IS NULL OR source = $14::text)
  AND ($15::text IS NULL OR artist = $15::text)
  AND ($16::text[] IS NULL OR tags @> $16::text[])
  AND ($17::text[] IS NULL OR NOT (tags && $17::text[]))
  AND ($18::bigint IS NULL OR id < $18::bigint)
ORDER BY id DESC
LIMIT $19
`

// SearchPostsScoreSQL is the score-ordered query contract. Parameters $1..$17
// mirror SearchPostsSQL; $18 is the cursor score, $19 is the cursor id, and
// $20 is limit+1. Ordering is deterministic score DESC then id DESC.
const SearchPostsScoreSQL = `
SELECT
    id::text,
    preview_url,
    original_url,
    media_type,
    width,
    height,
    tags,
    score
FROM posts
WHERE deleted_at IS NULL
  AND moderation_state = 'published'
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
  AND ($9::text IS NULL OR
       ($9::text = '>' AND file_size > $10::bigint) OR
       ($9::text = '>=' AND file_size >= $10::bigint) OR
       ($9::text = '<' AND file_size < $10::bigint) OR
       ($9::text = '<=' AND file_size <= $10::bigint) OR
       ($9::text = '=' AND file_size = $10::bigint))
  AND ($11::text IS NULL OR
       ($11::text = '>' AND id > $12::bigint) OR
       ($11::text = '>=' AND id >= $12::bigint) OR
       ($11::text = '<' AND id < $12::bigint) OR
       ($11::text = '<=' AND id <= $12::bigint) OR
       ($11::text = '=' AND id = $12::bigint))
  AND ($13::text IS NULL OR media_type = $13::text)
  AND ($14::text IS NULL OR source = $14::text)
  AND ($15::text IS NULL OR artist = $15::text)
  AND ($16::text[] IS NULL OR tags @> $16::text[])
  AND ($17::text[] IS NULL OR NOT (tags && $17::text[]))
  AND ($18::integer IS NULL OR (score < $18::integer OR (score = $18::integer AND id < $19::bigint)))
ORDER BY score DESC, id DESC
LIMIT $20
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
  AND moderation_state = 'published'
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

const insertUploadedPostSQL = `
INSERT INTO posts (
    preview_url, original_url, media_type, width, height, search_text,
    source, artist, hash, file_size, tags
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
RETURNING id::text
`

const lockPostForTagEditSQL = `
SELECT tags, search_text, tag_version
FROM posts
WHERE id = $1::bigint AND deleted_at IS NULL AND moderation_state = 'published'
FOR UPDATE
`

const updatePostTagsSQL = `
UPDATE posts
SET tags = $1, search_text = $2, tag_version = $3
WHERE id = $4::bigint AND deleted_at IS NULL AND moderation_state = 'published'
`

const insertPostRevisionSQL = `
INSERT INTO post_tag_revisions (post_id, version, kind, added_tags, removed_tags, target_tags)
VALUES ($1::bigint, $2, 'tag_edit', $3, $4, $5)
`

const lockPostForTagRevertSQL = `
SELECT tags, search_text, tag_version
FROM posts
WHERE id = $1::bigint AND deleted_at IS NULL AND moderation_state = 'published'
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
WHERE id = $1::bigint AND deleted_at IS NULL AND moderation_state = 'published'
FOR UPDATE
`

const updatePostReactionSQL = `
UPDATE posts
SET favorite = $1, score = $2, reaction_version = $3
WHERE id = $4::bigint AND deleted_at IS NULL AND moderation_state = 'published'
`

const insertPostReactionRevisionSQL = `
INSERT INTO post_reaction_revisions (post_id, version, favorite, score)
VALUES ($1::bigint, $2, $3, $4)
`

const listCollectionsSQL = `
SELECT c.id::text, c.name, c.version, COALESCE(array_agg(cp.post_id::text ORDER BY cp.position, cp.post_id) FILTER (WHERE cp.post_id IS NOT NULL), ARRAY[]::text[])
FROM collections c
LEFT JOIN collection_posts cp ON cp.collection_id = c.id
LEFT JOIN posts p ON p.id = cp.post_id AND p.deleted_at IS NULL AND p.moderation_state = 'published'
WHERE cp.post_id IS NULL OR p.id IS NOT NULL
GROUP BY c.id, c.name, c.version
ORDER BY c.created_at, c.id
`

const insertCollectionSQL = `
INSERT INTO collections (name) VALUES ($1)
RETURNING id::text, name, version
`

const collectionExistsSQL = `SELECT 1 FROM collections WHERE id = $1::bigint`

// lockCollectionSQL serializes add/remove/reorder mutations on one
// collection row. Callers hold the returned transaction open until the
// membership change and version bump commit together.
const lockCollectionSQL = `SELECT version FROM collections WHERE id = $1::bigint FOR UPDATE`

// collectionVersionSQL reads the membership/order version inside the same
// snapshot that serves the page, so the cursor check and the rows cannot
// observe different states.
const collectionVersionSQL = `SELECT version FROM collections WHERE id = $1::bigint`

// bumpCollectionVersionSQL records one effective membership or order
// change. No-op mutations skip it so the version stays stable.
const bumpCollectionVersionSQL = `UPDATE collections SET version = version + 1 WHERE id = $1::bigint`

// addCollectionPostSQL appends visible members in request order. Ordinality
// binds each position to the caller's array order instead of the posts scan
// order, so page order is deterministic from the first insert.
const addCollectionPostSQL = `
INSERT INTO collection_posts (collection_id, post_id, position)
SELECT $1::bigint, p.id, COALESCE((SELECT MAX(position) + 1 FROM collection_posts WHERE collection_id = $1::bigint), 0) + u.ord
FROM unnest($2::bigint[]) WITH ORDINALITY AS u(id, ord)
JOIN posts p ON p.id = u.id AND p.deleted_at IS NULL AND p.moderation_state = 'published'
ON CONFLICT (collection_id, post_id) DO NOTHING
`

const collectionPostIDsSQL = `
SELECT post_id::text FROM collection_posts cp
JOIN posts p ON p.id = cp.post_id AND p.deleted_at IS NULL AND p.moderation_state = 'published'
WHERE cp.collection_id = $1::bigint
ORDER BY cp.position, cp.post_id
`

// searchCollectionPostsSQL is the versioned keyset page contract. $1 is the
// collection; $2/$3 are the cursor (position, post_id) key and are NULL on
// the first page; $4 is limit+1. Ordering is deterministic position then
// post_id so position ties paginate without gaps or duplicates.
const searchCollectionPostsSQL = `
SELECT
    cp.position,
    p.id::text,
    p.preview_url,
    p.original_url,
    p.media_type,
    p.width,
    p.height,
    p.tags
FROM collection_posts cp
JOIN posts p ON p.id = cp.post_id AND p.deleted_at IS NULL AND p.moderation_state = 'published'
WHERE cp.collection_id = $1::bigint
  AND ($2::bigint IS NULL OR cp.position > $2::bigint OR (cp.position = $2::bigint AND cp.post_id > $3::bigint))
ORDER BY cp.position, cp.post_id
LIMIT $4
`

// collectionOrderedMembersSQL returns visible membership with positions in
// page order so reorder can verify the exact set and detect order no-ops.
const collectionOrderedMembersSQL = `
SELECT post_id::text, position FROM collection_posts cp
JOIN posts p ON p.id = cp.post_id AND p.deleted_at IS NULL AND p.moderation_state = 'published'
WHERE cp.collection_id = $1::bigint
ORDER BY cp.position, cp.post_id
`

const deleteCollectionPostSQL = `
DELETE FROM collection_posts cp USING posts p
WHERE cp.collection_id = $1::bigint AND cp.post_id = $2::bigint AND p.id = cp.post_id AND p.deleted_at IS NULL AND p.moderation_state = 'published'
`

// lockPostForModerationSQL intentionally filters on deleted_at only: a
// moderation transition must reach pending, hidden, and rejected rows.
// Deleted rows stay indistinguishable from missing rows.
const lockPostForModerationSQL = `
SELECT moderation_state
FROM posts
WHERE id = $1::bigint AND deleted_at IS NULL
FOR UPDATE
`

const updatePostModerationSQL = `
UPDATE posts
SET moderation_state = $1
WHERE id = $2::bigint AND deleted_at IS NULL
`

const insertModerationActionSQL = `
INSERT INTO post_moderation_actions (post_id, action, previous_state, new_state, actor, reason)
VALUES ($1::bigint, $2, $3, $4, $5, $6)
`

// moderationActionsByPostSQL returns the newest audit entries for one post.
// The post itself must be non-deleted; hidden and rejected posts keep their
// history for capability-checked readers.
const moderationActionsByPostSQL = `
SELECT id::text, post_id::text, action, previous_state, new_state, actor, reason, created_at::text
FROM post_moderation_actions
WHERE post_id = $1::bigint
  AND EXISTS (SELECT 1 FROM posts p WHERE p.id = $1::bigint AND p.deleted_at IS NULL)
ORDER BY created_at DESC, id DESC
LIMIT 50
`

// moderationQueueSQL lists non-deleted posts in one moderation state,
// newest first. The state parameter is validated by ValidateModerationQueue.
const moderationQueueSQL = `
SELECT
    id::text,
    preview_url,
    original_url,
    media_type,
    width,
    height,
    tags,
    moderation_state
FROM posts
WHERE deleted_at IS NULL
  AND moderation_state = $1::text
ORDER BY id DESC
LIMIT $2
`

// moderatedPostExistsSQL gates audit reads on the non-deleted post row so
// deleted posts stay indistinguishable from missing posts.
const moderatedPostExistsSQL = `SELECT 1 FROM posts WHERE id = $1::bigint AND deleted_at IS NULL`

// mediaPostStatesSQL maps a /media/* URL path to every referencing post row
// so the HTTP layer can deny hidden, rejected, pending, and deleted media.
// Duplicate content-addressed uploads may share one URL; visibility holds
// when any non-deleted referencing post is published. Derivative variant
// URLs are covered through asset_variants, including variants not
// currently selected as a post's preview, so hidden originals never leak
// thumbnails either.
const mediaPostStatesSQL = `
SELECT moderation_state, (deleted_at IS NULL)
FROM posts
WHERE preview_url = $1::text OR original_url = $1::text
UNION ALL
SELECT p.moderation_state, (p.deleted_at IS NULL)
FROM asset_variants v
JOIN posts p ON p.id = v.post_id
WHERE v.url = $1::text
`

// readyVariantURLSQL returns the served thumbnail URL for one post once its
// default derivative is ready. Posts without a ready variant fall back to
// their original URL as the preview.
const readyVariantURLSQL = `
SELECT url
FROM asset_variants
WHERE post_id = $1::bigint
  AND variant = 'thumb-320'
  AND status = 'ready'
`

// readyVariantURLsForPostsSQL returns ready default thumbnails for a batch
// of posts so search results overlay previews without per-row queries.
const readyVariantURLsForPostsSQL = `
SELECT post_id::text, url
FROM asset_variants
WHERE post_id = ANY($1::bigint[])
  AND variant = 'thumb-320'
  AND status = 'ready'
`

// derivativeJobForPostSQL finds the durable derivative job backing one
// post's default thumbnail through its idempotency key.
const derivativeJobForPostSQL = `
SELECT id::text, status
FROM jobs
WHERE kind = 'derivative'
  AND idempotency_key = $1::text
`
