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
    tags
FROM posts
WHERE deleted_at IS NULL
  AND id = $1::bigint
`
